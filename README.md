# 1BRC Top Solutions Learning Notes

Use Java to process 1 billion rows in less than 3 seconds on the official 8-core leaderboard.

這個 repo 是 1BRC 頂尖解法的學習版。重點是理解：為什麼 Java 可以把十億列文字資料，在官方硬體上壓到 1.5 到 3 秒左右。

## 任務

輸入檔案是 `measurements.txt`。

每列格式：

```text
station-name;temperature
```

範例：

```text
Hamburg;12.0
Istanbul;6.2
Conakry;31.2
```

目標是依測站名稱分組，輸出每個測站的：

```text
min/average/max
```

輸出需依測站名稱排序。

## 資料大小

官方挑戰使用 **1,000,000,000 rows**。

測站名稱最多約 **10,000 unique stations**。

溫度固定為 **一位小數**，所以頂尖解法多半用 fixed-point integer，例如 `12.3 -> 123`。

檔案大小會依測站名稱長度變動，通常是十幾 GB 等級。

## 評測硬體

官方 leaderboard 結果使用同一台 evaluation machine，因此不同解法的時間才可比較。

主 leaderboard 使用 **8 cores** 執行。

機器規格：

```text
Hetzner AX161 dedicated server
CPU: AMD EPYC 7502P, 32 cores / 64 threads, Zen2, 2.5 GHz
RAM: 128 GB ECC DDR4
OS: CentOS 9, Linux 5.14
Filesystem: EXT4
Input location: RAM disk
SMT: off
Turbo Boost: off
```

## 常用指令

建立測試資料：

```bash
./create_measurements.sh 1000000000
```

快速驗證：

```bash
./mvnw --quiet -Dquick verify
```

測試單一解法：

```bash
./test.sh thomaswue
./test.sh artsiomkorzun
./test.sh gonix
```

評測多個解法：

```bash
./evaluate.sh thomaswue artsiomkorzun jerrinot
```


## 核心處理管線

頂尖解法大多遵循同一流程：

1. Memory-map `measurements.txt`。
2. 把檔案切成多個 chunk。
3. 把 chunk 邊界移到 newline。
4. 每個 worker 直接解析 byte。
5. 每個 worker 用自己的 custom hash table 累積統計。
6. 所有 worker 完成後再合併。
7. 最後才建立 `String` 並排序輸出。

## 主題 1：Memory-Mapped File Access

問題：十億列資料太大，不能用一般逐行讀取方式。

一般寫法會像這樣：

```java
Files.lines(path)
```

這會建立大量 Java 物件，並經過字元解碼、iterator、lambda、split 等成本。

頂尖解法改用 memory mapping：

```java
FileChannel.map(FileChannel.MapMode.READ_ONLY, 0, fileSize, Arena.global())
```

它讓作業系統把檔案映射到記憶體。程式取得起始 address 後，就能像掃描一段連續 byte memory 一樣讀取。

常見 hot loop 是一次讀 8 bytes：

```java
long word = UNSAFE.getLong(address);
```

這樣可以避免大量 read call、buffer copy、逐行字串建立。

先看：`CalculateAverage_gonix.java` 的 `buildChunks()`，再看 `CalculateAverage_thomaswue.java` 的 memory address parsing。

## 主題 2：切塊與平行處理

問題：單一 thread 無法在幾秒內處理十億列。

做法是把檔案切成多個 byte range，交給多個 worker 同時解析。

但不能直接用任意 byte offset 切，因為 offset 可能落在一列中間。

錯誤切法：

```text
London;12.3\nTaipei;25.1\n
         ^ chunk boundary in the middle of a row
```

正確切法是把 chunk start/end 移到 newline。

常見策略有兩種：

1. Static split：每個 core 一段。
2. Work stealing：切成很多小 chunk，worker 透過 atomic counter 領下一段。

Work stealing 比較能處理不平均資料，例如某些 chunk 有較長 station name 或較多 hash collision。

先看：`CalculateAverage_gonix.java` 的 chunk boundary；再看 `CalculateAverage_thomaswue.java` 的 `AtomicLong cursor`。

## 主題 3：Unsafe 與 Foreign Memory

問題：Java 安全 API 會做邊界檢查和物件包裝，對十億列 hot loop 來說太貴。

頂尖解法用 `Unsafe` 直接讀 memory address：

```java
byte b = UNSAFE.getByte(address);
long word = UNSAFE.getLong(address);
```

Foreign Memory API 則常用來取得 mapped file 的 address。

這讓程式更像 C 程式：直接用 address 前進，自己保證不要讀錯。

好處是速度快。壞處是風險高。

錯誤 address 可能讓 JVM crash，或讀到不該讀的 memory。

先看：`CalculateAverage_gonix.java` 理解安全版本；再看 `CalculateAverage_gonixunsafe.java` 理解 off-heap unsafe 版本。

## 主題 4：SWAR 位元組掃描

問題：逐 byte 找 `;` 很慢。

一般想法：

```java
while (byteAt(pos) != ';') pos++;
```

這每個 byte 都有 branch。

SWAR 的做法是一次讀一個 `long`，也就是 8 bytes，然後用 bit trick 找其中是否有 `;`。

找 semicolon 常用 pattern：

```text
0x3B3B3B3B3B3B3B3B
```

找 newline 常用 pattern：

```text
0x0A0A0A0A0A0A0A0A
```

概念是：把 8 個 byte 同時和目標 byte 比較，產生 mask，再用 `Long.numberOfTrailingZeros()` 找位置。

這能減少 branch misprediction，也能讓 CPU 一次處理更多資料。

先看：`CalculateAverage_gonix.java` 的 `valueSepMark()`；再看 `CalculateAverage_abeobk.java` 的 `getSemiCode()`。

## 主題 5：Vector API

問題：SWAR 一次處理 8 bytes，但 CPU SIMD register 可以處理更多 bytes。

Java Vector API 用 `ByteVector` 表達 SIMD 操作。

常見用途：

```java
ByteVector.fromMemorySegment(...).compare(VectorOperators.EQ, ';')
```

用途有三個：

1. 找 `;`。
2. 找 `\n`。
3. 比較短 station name 是否相同。

但 Vector API 不是自動加速。若每次 vector 操作前後還要建立物件、做複雜分支、或處理很多 fallback，速度可能變差。

先看：`CalculateAverage_merykittyunsafe.java` 的 vector delimiter search；再看 `CalculateAverage_linl33.java` 如何用 vector 找 newline。

## 主題 6：Fixed-Point 溫度解析

問題：`Double.parseDouble()` 是通用 parser，太慢。

1BRC 的溫度格式固定：

```text
N.N
NN.N
-N.N
-NN.N
```

所以可以把溫度轉成十分之一度整數。

例如 `12.3` 解析為 `123`，`-4.5` 解析為 `-45`。

統計時只做 integer math：

```text
min = min(min, value)
max = max(max, value)
sum += value
count++
```

最後輸出才轉成小數。

最快 parser 會一次讀取溫度附近的 bytes，用 bit operation 找 `.`，抽出數字，再用乘法組合成整數。

先看：`CalculateAverage_merykittyunsafe.java` 的 `parseDataPoint()`；它的註解最適合學。

## 主題 7：客製化 Hash Table

問題：`HashMap<String, Stats>` 對這題太通用。

慢的原因：

1. 每列建立 `String`。
2. 每列計算 `String.hashCode()`。
3. `HashMap` entry 是物件，會 pointer chasing。
4. `Stats` 通常也是物件。
5. 十億次更新會造成大量 allocation 或 cache miss。

題目的限制是：unique station 最多約 10,000。

所以可以寫固定大小 custom table。

常見 layout：

```text
[key bytes/address][length][min][max][sum][count]
```

常見查找方式：

```text
index = hash & mask
if occupied and not equal: index++
```

先看：`CalculateAverage_gonix.java` 的 `int[] index + long[] mem`；再看 `CalculateAverage_artsiomkorzun.java` 的 off-heap 128-byte entry table。

## 主題 8：測站名稱處理

問題：資料有十億列，但 station name 只有約 10,000 種。

如果每列都建立 `String`，就會建立十億個字串，直接失敗。

頂尖解法只在 hot loop 中保留 raw name。

常見做法：

```text
address = station name start
length = station name length
firstLong = first 8 bytes
secondLong = next 8 bytes
```

短名稱只比較一到兩個 `long`。

長名稱才繼續比較更多 8-byte words。

最後只有輸出 unique station 時才把 bytes 轉成 `String`。

先看：`CalculateAverage_yavuztas.java` 的 `Record`；它存 address、length、word1、word2、wordLast。

## 主題 9：Worker-Local Aggregation 與最後合併

問題：如果所有 worker 同時更新同一個 global map，會很慢。

原因是 shared write 會造成：

1. lock contention。
2. cache line bouncing。
3. memory fence。
4. concurrent map overhead。

所以頂尖解法讓每個 worker 有自己的 table。

解析時：

```text
worker 0 -> table 0
worker 1 -> table 1
worker 2 -> table 2
```

解析完成後才 merge：

```text
final min = min(all worker mins)
final max = max(all worker maxes)
final sum = sum(all worker sums)
final count = sum(all worker counts)
```

先看：`CalculateAverage_merykittyunsafe.java` 的 `resultList`；再看 `CalculateAverage_linl33.java` 的 async merge。

## 主題 10：Native Image 與 JVM 調校

問題：官方計時包含 process startup。

如果程式本體只跑幾秒，JVM 啟動、JIT warmup、GC safepoint、memory unmap 都會變明顯。

GraalVM native image 可以把程式提前編譯成 binary，降低啟動與 warmup 成本。

部分解法還會啟動 worker subprocess：

```text
parent process -> start worker
worker -> compute and print result
parent -> receive output and return early
```

這能避免 parent 等待 memory unmap cleanup。

常見 flags 包含 `--enable-preview`、`--add-modules=jdk.incubator.vector`、`--enable-native-access=ALL-UNNAMED`、`-XX:-TieredCompilation`。

重點：這些是最後 5% 到 20% 的調校，不是主要演算法。

先看：`prepare_thomaswue.sh`、`prepare_artsiomkorzun.sh`、`calculate_average_serkan-ozal.sh`。
