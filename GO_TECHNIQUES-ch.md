# Go 1BRC 實作技巧

這個 repository 加入了一個 Go 實作：`src/main/go/calculate_average.go`，並提供一般 1BRC 慣用的 runner script：

```bash
./prepare_golang.sh
./calculate_average_golang.sh
./test.sh golang
```

目標是用接近 raw memory processing 的 hot path，和最快的 Java 風格解法競爭。在逐列處理迴圈中，這個實作不依賴 Go 方便但較通用的 `bufio.Scanner`、`strings.Split`、`strconv.ParseFloat` 或 `map[string]...`。

## 1. Native binary 取代 JVM 或 native image 調校

Go 本來就會 build 出 native executable，所以沒有 JVM startup、tiered compilation、JIT warmup，也不需要 GraalVM native-image 步驟。`prepare_golang.sh` 會 build 一個 stripped binary：

```bash
go build -trimpath -ldflags="-s -w" -o target/go/calculate_average_golang ./src/main/go
```

在 AMD64 上，如果使用者尚未設定 `GOAMD64`，script 預設使用 `GOAMD64=v3`。這讓 Go compiler 在現代 benchmark 機器上可以 targeting 較新的 x86-64 baseline，同時仍允許使用者覆寫成 `GOAMD64=v4` 或 `GOAMD64=v2`。

## 2. Memory-mapped input

程式使用 `syscall.Mmap` 將 `measurements.txt` 映射進記憶體，並把整個檔案當成一段連續的 `[]byte` 讀取。

這可以避免：

- 重複的 `read` syscall；
- 透過 user-space buffer 複製資料；
- 建立逐行物件；
- 在 hot path 做 UTF-8 decoding。

這個 mapping 在程式結束前刻意不主動 unmap。這是一個短生命週期的 benchmark process，程式退出時作業系統會回收 mapping。若在 timed path 內明確 unmap，反而會增加 cleanup work。

## 3. 對齊 newline 的 chunks

mapped file 會被切成多個 byte range。每個 range 的結尾會往前移到下一個 newline，確保沒有 worker 從一列中間開始或在一列中間停止。

chunk 數量基於 `GOMAXPROCS * 32`，因此 chunks 會比 workers 多。當某些 chunks 有較長的 station name 或較差的 hash-table collision pattern 時，這能讓 worker 負載更平均。

## 4. Goroutine workers 與 atomic chunk claiming

Workers 透過單一 atomic counter 領取下一個 chunk。hot path 裡沒有 channel。Channel 很符合 Go 的寫法，但對這種 workload 來說，它的同步與 queueing overhead 並不必要。

Worker 數量預設為 `runtime.GOMAXPROCS(0)`，也可以覆寫：

```bash
BRC_WORKERS=8 ./calculate_average_golang.sh
```

## 5. Worker-local aggregation

每個 worker 都擁有自己的 private hash table。逐列處理迴圈不會寫入 shared hash map，不需要 lock，不需要每列做 atomic，也不會在 workers 之間造成 cache-line bouncing。

只有在所有 parsing 完成後，程式才會把 worker-local tables merge 成一個 final table。

## 6. 客製化 fixed-size hash table

這個 challenge 大約只有 10,000 個 unique stations。Go 實作針對每個 worker 使用 open-addressed table，每張 table 有 `1 << 15` 個 slots。

每個 table entry 儲存：

```text
hash, file offset, name length, min, max, sum, count
```

Station name bytes 仍留在 mmap data 裡。Table 只儲存 offset 和 length。Collision 使用 linear probing 解決，最後再用 byte equality check 確認 key 是否相同。

這避免了 `map[string]Stats` 的通用成本：

- 每列建立 string；
- 通用 hash-map control flow；
- pointer-heavy buckets；
- 額外 allocations；
- global-map synchronization。

## 7. Raw station-name handling

Parser 不會為輸入列建立 `string`。Station key 以這種形式表示：

```text
data[offset : offset+length]
```

只有最後輸出 unique stations 時才會寫出文字。排序時也直接比較 mmap region 中的 raw UTF-8 bytes。

## 8. SWAR semicolon search

Station-name delimiter 使用 8-byte SWAR scan 尋找。每次迴圈載入一個 `uint64`，和八個 semicolon bytes 做 XOR，然後使用經典 zero-byte test：

```text
(x - 0x0101010101010101) & ~x & 0x8080808080808080
```

當 mask 非零時，`bits.TrailingZeros64(mask) / 8` 就是 delimiter 的 byte index。

這能減少 station-name 欄位中 branch-heavy 的逐 byte 掃描。

## 9. Unsafe unaligned 64-bit loads

`load64` 使用 `unsafe.Pointer` 從 mapped byte slice 讀取 `uint64`。這避免 helper-call overhead，並讓 delimiter scanning 和 hashing 都能一次處理一個 machine word。

這是有意識地針對架構做取捨。目標 leaderboard 等級的硬體是現代 64-bit Linux 機器，在這類 workload 中，unaligned 64-bit loads 已經足夠有效率。

## 10. Word-at-a-time station hashing

Station names 以 8-byte chunks 做 hash。這個 hash 不是 cryptographic hash；它的目標是在 power-of-two table 中，把大約 10,000 個 station names 分散得夠好。每次 candidate match 仍會做 byte equality check，所以 hash collision 不會破壞結果正確性。

## 11. Fixed-point temperature parsing

Temperature 會被解析成十分之一度整數：

```text
12.3  ->  123
-4.5  ->  -45
```

Parser 只處理合法的 1BRC 格式：

```text
N.N
NN.N
-N.N
-NN.N
```

這避免了 `strconv.ParseFloat`、hot loop 裡的 floating-point work，以及 temporary substrings。

## 12. Integer stats

Hot-loop update 全部只做 integer 操作：

```text
min = min(min, value)
max = max(max, value)
sum += value
count++
```

Average formatting 只會在最後對每個 unique station 做一次。

## 13. Java-compatible average rounding

原始 challenge 期待 Java 風格的一位小數 rounding。Go 的 `math.Round` 對 halfway cases 會 away from zero，而 Java 的 `Math.round` 等價於 `floor(x + 0.5)`。

這個實作使用：

```go
int64(math.Floor(v + 0.5))
```

來計算 average tenths，以符合 Java output behavior。

## 14. Buffered final output

程式透過 1 MiB 的 `bufio.Writer` 寫出最後結果。Unique stations 大約只有 10,000 個，所以 final formatting 不是主要瓶頸，但 buffering 能讓 output behavior 更穩定，也避免大量小型 writes。

## 15. Optional runtime knobs

實用的 benchmark knobs：

```bash
GOMAXPROCS=8 BRC_WORKERS=8 ./calculate_average_golang.sh
GOGC=off ./calculate_average_golang.sh
GOAMD64=v4 ./prepare_golang.sh
```

`GOGC=off` 只應該在確認 hot path 幾乎 allocation-free 之後使用。如果未來修改造成每列 allocation，關閉 GC 可能會讓 memory usage 爆炸。

## 尚未使用的技巧

這個實作目前沒有包含 hand-written assembly SIMD。如果要嘗試擊敗絕對最快的 Java entries，下一個最可能的方向是用 AMD64 assembly 加速 delimiter search，甚至 station-name comparison。Pure Go 可以很接近，但 Go 目前沒有像 Java Vector API 那樣穩定的 high-level SIMD API。

這個實作也沒有針對官方已知 station list 做 special-case。它仍然是一個適用於合法 input files 的通用 1BRC parser。
