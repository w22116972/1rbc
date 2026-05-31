# 頂尖 1BRC 解法解析

本文件根據 `TOP_SOLUTIONS.md` 以及其中列出的原始碼整理而成。內容先依主題分類重要技能，再逐一說明每個頂尖解法如何運用這些技能。

這個問題本身很單純：讀取 `measurements.txt`，每一列格式如下：

```text
station-name;temperature
```

最後要針對每個測站輸出：

```text
min/average/max
```

真正困難的是資料量。資料有十億列時，一般 Java 寫法，例如 `Files.lines()`、`String.split()`、`Double.parseDouble()`、`HashMap<String, ...>`，會花太多時間在物件配置、文字解碼、邊界檢查與記憶體同步上。最快的解法把檔案視為位元組資料，只解析必要內容，並且在熱路徑中幾乎不配置物件。

## 排行榜範圍

| 排名 | 時間 | 解法 | 主要檔案 |
| --- | ---: | --- | --- |
| 1 | 00:01.535 | thomaswue | `CalculateAverage_thomaswue.java` |
| 2 | 00:01.587 | artsiomkorzun | `CalculateAverage_artsiomkorzun.java` |
| 3 | 00:01.608 | jerrinot | `CalculateAverage_jerrinot.java` |
| 4 | 00:01.880 | serkan-ozal | `CalculateAverage_serkan_ozal.java` |
| 5 | 00:01.921 | abeobk | `CalculateAverage_abeobk.java` |
| 6 | 00:02.018 | stephenvonworley | `CalculateAverage_stephenvonworley.java` |
| 7 | 00:02.157 | royvanrijn | `CalculateAverage_royvanrijn.java` |
| 8 | 00:02.319 | yavuztas | `CalculateAverage_yavuztas.java` |
| 9 | 00:02.332 | mtopolnik | `CalculateAverage_mtopolnik.java` |
| 10 | 00:02.367 | merykittyunsafe | `CalculateAverage_merykittyunsafe.java` |
| 11 | 00:02.507 | gonixunsafe | `CalculateAverage_gonixunsafe.java` |
| 12 | 00:02.557 | yourwass | `CalculateAverage_yourwass.java` |
| 13 | 00:02.820 | linl33 | `CalculateAverage_linl33.java` |
| 14 | 00:02.995 | tivrfoa | `CalculateAverage_tivrfoa.java` |
| 15 | 00:02.997 | gonix | `CalculateAverage_gonix.java` |

## 整體思考模型

幾乎所有頂尖解法都遵循同一條處理管線：

1. 將 `measurements.txt` 做 memory map。
2. 把映射後的檔案切成多個區塊。
3. 調整區塊邊界，確保每個 worker 都從完整的一列開始、在完整的一列結束。
4. 每個 worker 直接解析位元組。
5. 每個 worker 使用自己的客製化表格累積測站統計。
6. 解析完成後才合併各 worker 的結果。
7. 只有在最後排序輸出時，才把測站名稱轉成 `String`。

這個架構比單一技巧更重要。最快的程式之所以快，是因為熱迴圈避開了通用 API。

## 主題 1：Memory-Mapped File Access

### 技能

使用 memory mapping，讓作業系統把檔案分頁映射到記憶體中，程式可以把它當成連續的位元組範圍掃描。這能避免反覆呼叫 read，也讓 worker 可以使用類似指標的位址。

### 常見寫法

多數頂尖解法使用：

```java
FileChannel.map(FileChannel.MapMode.READ_ONLY, 0, fileSize, Arena.global())
```

接著用 `Unsafe.getByte()` 或 `Unsafe.getLong()` 從映射位址讀取資料。

### 為什麼有效

解析器可以一次讀取 8 個位元組。例如掃描測站名稱時，可以載入一個 `long`，檢查其中是否包含 `;`，如果沒有就往前移動 8 個位元組。

### 使用此技巧的解法

`thomaswue`、`artsiomkorzun`、`jerrinot`、`serkan-ozal`、`abeobk`、`stephenvonworley`、`royvanrijn`、`yavuztas`、`mtopolnik`、`merykittyunsafe`、`gonixunsafe`、`yourwass`、`linl33`、`tivrfoa` 都使用 memory mapping 搭配直接位址存取或 foreign memory segment。

`gonix` 使用 `MappedByteBuffer`，比原始 `Unsafe` 較安全、較容易理解，但速度稍慢。

## 主題 2：切塊與平行處理

### 技能

把檔案拆開，讓所有 CPU 核心同時解析，同時確保區塊不會把一列資料切成兩半。

### 兩種主要策略

靜態平均切分：

每個 worker 拿到一個大型區域。這很簡單，而且通常已經足夠快。

動態 work stealing：

worker 透過 atomic counter 或共享 queue 領取下一個區塊。這能更好地平衡不均勻成本，因為有些區塊的測站名稱較長，或 hash collision 較多。

### 邊界處理

原始 byte offset 可能落在一列中間，因此解法會把起點或終點移到下一個 newline。如此可確保每列剛好被處理一次。

### 範例

`thomaswue` 使用 2 MB segment 和 `AtomicLong` cursor。每個 worker 反覆領取下一個 segment。每個 segment 內部又被拆成三個子範圍，讓同一個 thread 同時推進三個 cursor。

`artsiomkorzun` 使用 2 MB segment 和 `AtomicInteger` segment counter。

`jerrinot` 使用 global cursor 和 4 MB segment。

`stephenvonworley` 把 chunk 放進 `ConcurrentLinkedDeque`，worker 持續 poll 直到沒有 chunk。

`serkan-ozal` 建立 256 個 region，並讓固定大小 thread pool 從 concurrent queue 中消耗任務。

`mtopolnik`、`merykittyunsafe`、`yourwass`、`linl33`、`royvanrijn`、`gonix` 使用較靜態的切分方式。

`tivrfoa` 使用混合式 chunk 規劃：檔案前段使用較大 chunk，後段使用較小 chunk，worker 透過 atomic index 領取工作。

## 主題 3：Unsafe 與 Foreign Memory

### 技能

用很低的 API overhead 直接從記憶體讀取 byte 和 long。

### 為什麼頂尖解法使用它

一般 Java array 或 buffer 存取包含安全檢查。這些檢查在正式產品中很有價值，但在這個挑戰中會耗時。`Unsafe.getLong(address)` 可以用較少 overhead 從映射檔案讀取 8 個位元組。

### 取捨

這不是產品環境中建議的 Java 寫法。位址錯誤可能讓 JVM 崩潰。有些解法也依賴 little-endian CPU 行為。

### 範例

`artsiomkorzun` 用 `Unsafe.allocateMemory()` 把整個客製化 aggregation table 放在 off heap。

`jerrinot` 有分開的 fast map 和 slow map，底層由 raw memory 支撐。

`gonixunsafe` 把 index 和 station record 存在手動配置的記憶體中。

`linl33` 使用 Java 22 foreign memory，並透過 foreign function API 呼叫 native `malloc` 和 `calloc`。

`gonix` 是很好的對照組：它較接近一般 Java，使用 `MappedByteBuffer`、`ByteBuffer`、`long[]`、`int[]`。

## 主題 4：SWAR 位元組掃描

### 技能

用一個 machine word 同時測試多個 byte。SWAR 是 "SIMD within a register"。程式仍是 scalar Java，但 bit operation 把一個 `long` 當成八個小 lane 使用。

### 分隔符偵測

為了找 `;`，很多解法載入 8 個 byte，並和下列 pattern 比較：

```text
0x3B3B3B3B3B3B3B3B
```

結果會指出該 word 中是否有任何 byte 是 `;`。同樣想法也可用來找 newline：

```text
0x0A0A0A0A0A0A0A0A
```

### 為什麼有效

程式不需要逐 byte 分支判斷，而是用算術和 bit mask 一次檢查 8 個 byte。這能減少 branch misprediction，並讓 CPU 每次 load 做更多有用工作。

### 範例

`thomaswue`、`jerrinot`、`abeobk`、`yavuztas`、`mtopolnik`、`gonixunsafe`、`gonix`、`tivrfoa` 都大量使用此技巧。

`thomaswue` 和 `stephenvonworley` 還在 chunk 內同時解析三個 cursor。這讓 CPU 有更多彼此獨立的工作，可隱藏延遲。

## 主題 5：Vector API

### 技能

使用 `jdk.incubator.vector.ByteVector` 一次比較多個 byte。

### 適用場景

Vector API 特別適合：

1. 尋找 `;` 或 `\n`。
2. 比較短測站名稱。
3. 處理符合 vector size 對齊的 batch。

### 範例

`serkan-ozal` 選擇 128-bit vector，因為實驗顯示多數測站名稱很短，較寬的 vector 不一定更有效。

`merykittyunsafe` 使用 vector 尋找 semicolon，並比較 custom map 中的測站名稱。

`yourwass` 使用 vector 尋找 delimiter，並在名稱可放入一個 vector 時比較 city name。

`linl33` 用 vector 找 newline 位置，再從 vector mask 中找出的完整 line 進行處理。

### 重要觀念

Vector API 不會自動變快。它最適合用在資料 layout 和解析迴圈已經圍繞 byte-level scanning 設計好的情況。

## 主題 6：Fixed-Point 溫度解析

### 技能

把溫度解析成「十分之一度」的整數，而不是 `double`。

例如：

```text
12.3  -> 123
-4.5  -> -45
```

只有最後輸出時才轉回一位小數。

### 為什麼有效

輸入格式固定且範圍小。溫度永遠只有一位小數，且有效範圍有限。整數運算更快、更容易累加，也避免浮點數解析。

### Branchless parser

許多解法使用 merykitty 推廣的 parser。它把溫度 byte 載入一個 `long`，找出小數點，移除正負號，抽出數字 nibble，乘上一個常數，再套用正負號。

高層概念如下：

1. 從 bit pattern 定位 `.`。
2. 對齊數字 byte。
3. 只保留低位 digit nibble。
4. 用一次乘法組合百位、十位、個位。
5. 不用分支套用正負號。

### 範例

`merykittyunsafe` 包含核心版本，且註解解釋得很清楚。

`abeobk`、`yavuztas`、`tivrfoa`、`gonix`、`gonixunsafe` 使用非常相似的 branchless parser。

`yourwass` 使用 lookup table。它預先計算整數部分和小數部分，解析時透過 table read 完成。

`linl33` 從每一列結尾解析，也就是讀取 `\n` 前面的 byte。這可行是因為溫度格式短且可預測。

## 主題 7：客製化 Hash Table

### 技能

用針對最多 10,000 個測站名稱設計的特殊表格，取代 `HashMap<String, Stats>`。

### 為什麼一般 `HashMap` 慢

`HashMap<String, Stats>` 需要建立 `String` key、配置物件、計算通用字串 hash、追蹤物件指標，還要處理 resize 邏輯。頂尖解法避開了大部分成本。

### 常見表格設計

多數 custom table 使用：

1. 2 的次方容量。
2. mask 取代 modulo。
3. open addressing 或 linear probing。
4. 用 primitive field 儲存 `min`、`max`、`sum`、`count`。
5. 儲存原始測站名稱 byte 或位址參照。

### 範例

`artsiomkorzun` 使用 64K entry 的 off-heap table，每個 entry 128 byte。

`jerrinot` 使用 fast map 和 slow map。短名稱可透過一到兩個 `long` 比對，長名稱走慢路徑。

`abeobk` 使用 `Node[]` table 搭配 linear probing，儲存 hash、第一個 word、key address、key length 和統計資料。

`mtopolnik` 使用 off-heap `StatsAccessor` table，並考慮 cache line 配置。

`merykittyunsafe` 使用 `byte[]` 支撐的 open-address table，每個 entry 128 byte。

`gonix` 使用 `int[]` 當 index，`long[]` 當緊湊 record storage。

`gonixunsafe` 把同樣概念移到 off heap。

`linl33` 使用 sparse table 做查找，dense table 做快速實際 entry 迭代。

## 主題 8：測站名稱處理

### 技能

延後建立 `String`，直到最終輸出。

### 為什麼有效

資料可能有十億列，但唯一測站名稱最多只有 10,000 個。每列都建立 `String` 會非常昂貴。頂尖解法在解析時用 byte 或 long 比較名稱，只在最後排序輸出唯一測站名稱時才建立字串。

### 快速名稱比對

短名稱很常見，因此很多解法優化前 8 或 16 個 byte：

1. 載入第一個 word。
2. 檢查是否有 `;`。
3. 如果找到，mask 掉 delimiter 後面的 byte。
4. 用該 word 作為 hash 與 equality check 的一部分。

較長名稱則 fallback 到額外的 8-byte word 比較。

### 範例

`thomaswue` 在 fast path 中儲存第一和第二個 name word，超過 16 byte 的名稱走慢路徑。

`jerrinot` 對短名稱使用 fast map，長名稱使用 slow map。

`yavuztas` 儲存 first word、second word、last word、length 和原始記憶體位址。

`yourwass` 儲存 city address 和 length，並在可行時用 vector-sized chunk 比較。

`linl33` 在 hash table 中儲存原始 name address 和 length。

## 主題 9：Worker-Local Aggregation 與最後合併

### 技能

避免在熱迴圈中寫入共享資料。

### 為什麼有效

如果每列都更新同一個 global concurrent map，程式會把時間浪費在 locking、cache-line contention 和 memory fence 上。頂尖解法讓每個 worker 更新自己的 map，解析完成後才合併。

### 合併策略

多數解法最後合併到 `TreeMap`，或排序緊湊的 entry list。

`gonixunsafe` 較特殊：worker 透過 `AtomicReference` 合併 `Aggregator` instance，減少最終合併步驟，同時保持解析階段的 local state。

`linl33` 用 `CompletableFuture.runAfterBothAsync()` 開始把 worker map 合併到 map 0。

`yourwass` 在每個 thread 把 local result 貢獻到共享 `TreeMap` 時使用 lock；這個 lock 不在逐列熱路徑中。

## 主題 10：Native Image 與 JVM 調校

### 技能

演算法已經緊湊後，再用 runtime 設定降低啟動與執行 overhead。

### GraalVM native image

幾個最快解法可以用 native image 執行。launcher script 會先檢查 `target/` 中是否有 image，沒有才 fallback 到 JVM mode。

Native image 有幫助，因為這個挑戰測量的是完整 process time，不只是 steady-state parsing time。

### Worker subprocess 技巧

有些解法會啟動 child process 做實際工作，並把輸出 pipe 回 parent。parent 收到輸出後即可返回，不必等待 memory unmapping cleanup。

使用者包括：`thomaswue`、`artsiomkorzun`、`jerrinot`、`abeobk`、`stephenvonworley`、`royvanrijn`、`yavuztas`、`mtopolnik`。

### JVM flags

launcher 使用的 flags 包含：

1. `--enable-preview`
2. `--add-modules=jdk.incubator.vector`
3. `--enable-native-access=ALL-UNNAMED`
4. `-XX:-TieredCompilation`
5. `-XX:+UseNUMA`
6. `-XX:+UseTransparentHugePages`
7. `-Djdk.incubator.vector.VECTOR_ACCESS_OOB_CHECK=0`

這些是最後的微調。它們救不了慢 parser，但當核心迴圈已經接近硬體極限時，會帶來差異。

## 各解法重點

### thomaswue

主要想法：

1. 支援 GraalVM native image。
2. 使用 worker subprocess 避免 memory-unmap 延遲。
3. 使用 memory-mapped file 與 foreign memory address access。
4. 以 2 MB segment 做動態 work stealing。
5. 每個 segment 內有三個 cursor。
6. 客製化 hash table。
7. SWAR delimiter detection。
8. Branchless integer temperature parsing。

這是列表中最快的解法。關鍵不只是依核心數切分檔案，而是使用大量小 segment，讓 worker 動態領取更多工作，以減少負載不均。三 cursor parser 也讓 CPU 得到更多獨立操作。

適合學習：整體架構、動態切塊、subprocess 技巧、極致熱迴圈設計。

### artsiomkorzun

主要想法：

1. 透過 `MemorySegment` 做 memory mapping。
2. 用 `Unsafe` 從映射記憶體讀取。
3. 用 atomic counter 做 2 MB segment work stealing。
4. 使用 128-byte entry 的 off-heap aggregation table。
5. 快速 word-based delimiter detection。
6. 使用 epsilon GC 與 aggressive optimization flags 準備 native image。

最突出的特色是 off-heap `Aggregates` table。它把測站 metadata 和統計資料存在 raw memory，而不是 Java object。這改善 locality，也讓 parser 不需要配置物件。

適合學習：raw memory table layout 與 worker-local off-heap aggregation。

### jerrinot

主要想法：

1. Worker subprocess。
2. Memory mapping 加上 `Unsafe`。
3. 使用 global cursor 掃描 4 MB segment。
4. 分開的 fast station map 與 slow station map。
5. 借用 merykitty 的 branchless parser。
6. 借用 mtopolnik 的 hashing 想法。
7. 借用 abeobk 的 lookup-table mask 想法。

這個解法很有價值，因為它明確組合多個勝出技巧。短測站名稱走緊湊 fast path，較長名稱走 slow path。這是常見的高效能設計：讓常見情況極快，並保持少見情況正確。

適合學習：fast-path/slow-path 設計，以及如何務實組合已知最佳化。

### serkan-ozal

主要想法：

1. 使用 Vector API 做 delimiter search 和 key comparison。
2. 可選 shared memory region。
3. 256 個 region 由 thread pool 消耗。
4. 可設定 platform thread 或 virtual thread。
5. 客製化 byte-array-backed result map。
6. Launcher script 中有大量 JVM 調校。

此解法使用 128-bit byte vector，因為實驗顯示多數測站名稱足夠短，較寬 vector 不一定有幫助。這提醒我們：vector 越寬不一定越快。

適合學習：Java Vector API、task queue 設計與 runtime tuning。

### abeobk

主要想法：

1. Worker subprocess。
2. Memory mapping 與 `Unsafe`。
3. 4 MB chunk。
4. 每個 worker 把 chunk 分成三個 parser。
5. 對 semicolon、newline、小數點提供 SWAR helper。
6. 使用 linear probing 的客製化 `Node[]` hash table。
7. Branchless temperature parsing。

相較部分頂尖解法，`abeobk` 相當精簡清楚。helper function 直接展示核心 byte-scanning 技巧：找 delimiter、找 newline、找 dot、parse number、mix hash。

適合學習：較易讀的 SWAR helper 和 chunk parsing。

### stephenvonworley

主要想法：

1. Worker subprocess。
2. Memory mapping。
3. Chunk queue。
4. 每個 worker 一張 table。
5. Chunk 內三路解析。
6. Off-heap table layout。
7. 支援 native image。

此解法在註解中清楚描述 pipeline。`parse3()` 策略和 `thomaswue` 精神相近：同一 thread 在 chunk 中處理三個獨立位置，以提高 instruction-level parallelism。

適合學習：清楚描述的架構與 three-cursor parsing。

### royvanrijn

主要想法：

1. Worker subprocess。
2. Memory mapping 加上 `Unsafe`。
3. 每個 processor 一個 segment。
4. Flyweight `byte[]` entry。
5. 使用 `ConcurrentHashMap` 做最後合併。
6. 延後建立字串。
7. 有詳盡 changelog 展示最佳化過程。

這個檔案很有用，因為註解記錄了從慢實作到快實作的過程。它列出跳過 string creation、加入 memory mapping、使用 custom map、使用 SWAR token check、改善 layout 等步驟。

適合學習：效能迭代過程，以及理解哪些最佳化真的有影響。

### yavuztas

主要想法：

1. Worker subprocess。
2. 在 memory-mapped file 上使用 `Unsafe`。
3. 使用兩倍於 processor 數量的 region 來改善平衡。
4. 客製化 `RecordMap`。
5. Record 儲存原始 address、length、first words、last word、hash 和 stats。
6. 使用 linked list 處理 collision。
7. Branchless temperature parsing。

這個解法每個 worker 對每個唯一測站建立一個 `Record` object，而不是每列都建立物件。相對一般解析仍然是低配置量。名稱 equality check 使用 long 比較而不是逐 byte 比較。

適合學習：稍微物件導向但仍然快速的 custom map。

### mtopolnik

主要想法：

1. Worker subprocess。
2. 依 processor count 做靜態 chunk split。
3. Memory-mapped file 搭配 `Unsafe` 讀取。
4. 每個 worker 配置 off-heap stats table。
5. 對一個或兩個 word 以內的短名稱提供 fast path。
6. 最終輸出使用 merge-sort-like 方式合併已排序 worker result。

解析器特別處理長度 8 和 16 byte 以內的名稱，較長名稱才 fallback。表格把測站名稱存在 off-heap slot，並以 word 比較。

適合學習：短名稱 specialization 與 cache-aware table design。

### merykittyunsafe

主要想法：

1. Memory mapping。
2. `Unsafe` 與 Java Vector API。
3. Worker-local `PoorManMap`。
4. `byte[]` 中的 128-byte entry。
5. Vector delimiter search 與 vector key comparison。
6. Branchless fixed-point temperature parser。
7. 對 tail 使用簡單 scalar fallback。

這是學習著名 branchless temperature parser 的最佳檔案之一，因為註解解釋了 digit packing 和 multiplication。它也展示了用普通 byte array 做 custom map 的乾淨設計。

適合學習：溫度解析與 vector-assisted map lookup。

### gonixunsafe

主要想法：

1. Memory mapping。
2. 使用 `Unsafe` 配置 off-heap memory。
3. Chunk 對齊 newline boundary。
4. 使用分離 index 和連續 record storage 的 poor-man hash map。
5. 使用 padded tail handling 避免在 mapping 結尾附近 unsafe over-read。
6. 使用 AtomicReference-based aggregator merging。

這是 `gonix` 設計的 unsafe 版本。它保留相同的緊湊 table 想法，但把 storage 移到 off heap，並用 raw address 讀取。

適合學習：緊湊資料 layout、tail safety，以及如何把 buffer-based solution 轉成 off-heap。

### yourwass

主要想法：

1. Memory mapping 與 `Unsafe`。
2. 使用 Vector API 做 delimiter search 與 short-name comparison。
3. 每個 thread 有自己的 raw memory result area。
4. 用 lookup table 解析溫度。
5. Station key 儲存為 address 加 length。
6. 最後在 lock 保護下合併到 `TreeMap`。

不尋常之處是用 precomputed lookup table 解析溫度，而不是 branchless multiplication parser。這是用 indexed table read 換取算術運算。

適合學習：lookup-table parsing 與 vectorized short-key matching。

### linl33

主要想法：

1. Java 22 features。
2. 透過 foreign memory 做 memory mapping。
3. 使用 Vector API 找 newline 位置。
4. 使用 platform thread 的 thread pool。
5. 非同步合併到第一張 table。
6. Off-heap sparse/dense hash table。
7. 透過 foreign function call 使用 native `malloc`/`calloc`。

許多 parser 從測站名稱開頭往前掃到 `;`，但這個解法用 vector scanning 先找 line ending，再解析固定大小的溫度尾端。Hash table 儲存 raw address 和 length。

適合學習：newline-first parsing 與 sparse/dense table layout。

### tivrfoa

主要想法：

1. Memory mapping 搭配 `Unsafe`。
2. 使用預先計算的 chunk size 與 atomic chunk index 分配工作。
3. Custom bucket table。
4. XXH3 avalanche-style hash mixing。
5. Branchless temperature parser。
6. 最終合併到 `TreeMap`。

此解法明確基於早期 `thomaswue` 版本，並實驗 hash 和 chunk distribution。它很適合觀察 10,000 個唯一測站名稱時的 collision 行為。

適合學習：hash quality tradeoff 與 chunk scheduling 實驗。

### gonix

主要想法：

1. 使用 `MappedByteBuffer`，而不是 raw address access。
2. 對檔案 chunk 使用 parallel stream。
3. 使用緊湊的 `int[]` index 加 `long[]` record storage。
4. SWAR delimiter 與 decimal detection。
5. Fixed-point integer temperature parsing。
6. 把 tail 複製到 padded buffer，安全地進行 long read。

這是最容易入門的頂尖解法，因為它避開了大部分 raw `Unsafe` 風格。它仍然使用所有重要核心想法：切塊、一次解析一個 long、fixed-point 溫度、客製化 primitive table。

適合學習：先理解核心演算法，再進入 `Unsafe` 版本。

## 建議學習順序

1. 先讀 `gonix`，理解 chunking、SWAR parsing、fixed-point temperature 和 compact table，而且不需要先面對 raw pointer。
2. 再讀 `merykittyunsafe`，學習 temperature parser 和 vector-assisted key matching。
3. 讀 `abeobk`，理解精簡的 SWAR helper 和 worker chunk parsing。
4. 讀 `mtopolnik` 或 `yavuztas`，學習 custom table design 和 name equality。
5. 最後讀 `thomaswue`、`artsiomkorzun`、`jerrinot`，理解極限最佳化層。
6. 等演算法清楚後再讀 launcher 和 prepare scripts；runtime flags 是最後一層，不是基礎。

## 實用重點

可重用的技能如下：

1. 先設計資料路徑，再調校 runtime。
2. 避免在熱迴圈中配置物件。
3. 當格式固定時，以 byte 解析結構化文字。
4. 對已知精度的小數使用 fixed-point integer。
5. 保持每個 thread 的狀態 local。
6. 平行解析完成後再合併。
7. 只有真正需要時才建立字串。
8. 當輸入限制已知時，使用特殊化 hash table。
9. 使用 vector/SWAR 技術減少 branch-heavy byte scanning。
10. 在演算法已經有效率後，再套用 native image 與 JVM flags。

