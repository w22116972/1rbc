# 快速版 Go 1BRC Candidate

這份文件說明第二個 Go 實作：`src/main/go-fast/calculate_average.go`。

使用一般 1BRC fork naming convention 執行：

```bash
./prepare_golang_fast.sh
./calculate_average_golang_fast.sh
./test.sh golang_fast
```

這個實作是為了認真嘗試和最快 Java 解法競爭而寫。它把 Go 當成 native systems language 使用：mmap input、raw byte parsing、worker-local aggregation、custom hash tables、unsafe word loads、SWAR delimiter detection，而且 hot path 沒有 per-row allocations。是否能擊敗絕對最快的 Java entry，仍必須在官方 Linux AMD64 benchmark machine 上實測，但程式結構是針對那個目標設計。

## 1. 獨立的快速版 candidate

較早的 Go 實作仍保留在 `src/main/go`。更快的 candidate 放在 `src/main/go-fast`，讓 correctness 和 performance experiments 不會干擾較簡單的版本。

Runner name 是 `golang_fast`，所以既有 test harness 不需要修改：

```bash
./test.sh golang_fast
```

## 2. Native Go Binary

Go 會直接 build 成 native executable。沒有 JVM startup、JIT warmup、tiered compilation，也不需要 GraalVM native-image preparation。

`prepare_golang_fast.sh` 會 build stripped binary：

```bash
go build -trimpath -ldflags="-s -w" -o target/go/calculate_average_golang_fast ./src/main/go-fast
```

在 AMD64 上，如果環境沒有設定 `GOAMD64`，預設會使用 `GOAMD64=v3`。較新的機器可以在 benchmark 時嘗試 `GOAMD64=v4`。

## 3. Mmap 與 kernel advice

Input file 使用 `syscall.Mmap` 映射，並以一段連續的 `[]byte` 讀取。

實作也呼叫：

```go
syscall.Madvise(data, syscall.MADV_SEQUENTIAL)
```

Benchmark input 通常位於 RAM disk，所以這不是魔法開關。不過它仍是一個有用提示：程式會 sequentially 掃描這段 mapping。

程式結束前不明確 unmap。這是一個短生命週期的 batch process，process teardown 會回收 mapping，不需要在 timed path 增加 cleanup work。

## 4. 對齊 newline 的 chunking

檔案會被切成 byte ranges，而且每個 chunk end 會移到 newline。這保證沒有 worker 解析 partial row。

Chunk 數量是 `GOMAXPROCS * 32`。Chunks 比 workers 多，可以在 station names、hash collisions 或 cache behavior 隨輸入區域變化時提供 load balancing。

## 5. Atomic work stealing，沒有 channels

Workers 從一個 atomic counter 領取 chunk indexes。Parse path 中沒有 channels。

Channels 對一般 Go 程式很有用，但這裡每個 chunk 一次 atomic increment 更便宜，也已經足夠。

## 6. Worker-local hash tables

每個 worker 擁有自己的 hash table。Parsing 期間沒有：

- shared map writes；
- locks；
- per-row atomics；
- concurrent map overhead；
- station updates 的 cache-line bouncing。

所有 workers 完成後才 merge tables。

## 7. 較大的低負載率 table

快速版 candidate 對每個 worker 使用 `1 << 16` 個 slots。Unique stations 大約 10,000 個，這能讓 table load 保持較低，降低 linear-probing 長度。

這是刻意的 memory tradeoff。在 8-worker run 下，tables 使用數十 MiB；相對於官方 128 GiB 機器和 input file，這很小。

## 8. Single-pass station scan and hash

前一個 Go 版本先找 `;`，再重新掃描 station name 以計算 hash。

快速版 candidate 把這兩件事合併。`scanName` 每次讀一個 `uint64`，一邊找 `;`，一邊更新 station hash。

這移除了對每個 station name 的第二次完整掃描。

## 9. SWAR semicolon search

Delimiter search 使用經典的 8-byte zero-byte trick：

```text
(x - 0x0101010101010101) & ~x & 0x8080808080808080
```

程式會將每個 loaded word 和 `0x3b3b3b3b3b3b3b3b` 做 XOR，所以任何 semicolon 都會變成 zero byte。`bits.TrailingZeros64(mask) / 8` 就能得到 delimiter position。

這能減少 branch-heavy 的逐 byte scanning。

## 10. Unsafe unaligned word loads

`load64` 使用 `unsafe.Pointer` 直接從 mmap-backed byte slice 讀取 `uint64`。

目標是現代 64-bit Linux hardware，在這類 workload 裡 unaligned 64-bit loads 已經足夠有效率。這不是為了 portable Windows code 而寫，而是針對 1BRC benchmark class。

## 11. Station fingerprints

每個 hash-table entry 儲存：

```text
hash, first 8 bytes, second 8 bytes, last 8 bytes, offset, length, stats
```

對於長度最多 24 bytes 的 names，這三個 fingerprints 會覆蓋整個 station name。Matching entries 不需要 `bytes.Equal`。

對於超過 24 bytes 的 names，fingerprints 會快速排除幾乎所有 non-matches，然後再用 exact byte comparison 保持正確性。

## 12. Hot path 不建立 string

Parser 不會為每一列建立 `string`。Station keys 是 mmap data 裡的 offsets 和 lengths。

最後輸出也不需要建立 strings：writer 會直接寫出每個 unique station 的原始 bytes。排序時也直接比較 byte slices。

## 13. Fixed-point temperature parser

Temperatures 會被解析成十分之一度：

```text
12.3  ->  123
-4.5  ->  -45
```

只處理合法的 1BRC formats：

```text
N.N
NN.N
-N.N
-NN.N
```

Hot loop 不呼叫 `strconv.ParseFloat`，不 allocate substrings，也不做 floating-point math。

## 14. Integer-only Java rounding

Output average 必須符合 Java 的 `Math.round`，也就是：

```text
floor(x + 0.5)
```

快速版 candidate 避免 floating-point conversion，改用 integer arithmetic 計算 rounded average：

```go
numerator := sum*2 + int64(count)
denominator := int64(count) * 2
```

對 negative numerators，它會做 floor division，而不是使用 Go 預設的 toward-zero truncation。

## 15. Buffered output

Final output 使用 1 MiB 的 `bufio.Writer`。Stations 大約只有 10,000 個，但 buffering 可以讓 output overhead 更穩定。

## 16. Runtime knobs

實用的 benchmark commands：

```bash
GOMAXPROCS=8 BRC_WORKERS=8 ./calculate_average_golang_fast.sh
GOGC=off ./calculate_average_golang_fast.sh
GOAMD64=v4 ./prepare_golang_fast.sh
```

`GOGC=off` 只適合在 implementation 仍保持 hot path allocation-free 時使用。

## 17. 擊敗最佳 Java 的剩餘路徑

最可能的下一步優化是針對 delimiter search 和 station-name comparison 寫 AMD64 assembly SIMD。Pure Go 可以非常有競爭力，但 Go 目前沒有像 Java Vector API 那樣穩定的 high-level SIMD API。

這個實作刻意不針對官方 station list 做 special casing，所以它仍然是一個適用於合法 inputs 的通用 1BRC parser。
