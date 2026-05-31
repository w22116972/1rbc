# Top 1BRC Solutions Explained

This guide is based on `TOP_SOLUTIONS.md` and the listed source files. It groups the important skills by topic first, then explains how each top solution applies those skills.

The central problem is simple: read `measurements.txt`, where every row is:

```text
station-name;temperature
```

For each station, print:

```text
min/average/max
```

The hard part is scale. With one billion rows, normal Java patterns such as `Files.lines()`, `String.split()`, `Double.parseDouble()`, and `HashMap<String, ...>` spend too much time allocating objects, decoding text, checking bounds, and synchronizing memory access. The fastest submissions treat the file as bytes, parse only what is needed, and allocate almost nothing in the hot loop.

## Leaderboard Set

| Rank | Time | Solution | Main file |
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

## Mental Model

Almost every top solution follows this pipeline:

1. Memory-map `measurements.txt`.
2. Split the mapped file into chunks.
3. Adjust chunk boundaries so every worker starts and ends on full lines.
4. Let each worker parse bytes directly.
5. Store station statistics in a worker-local custom table.
6. Merge worker-local results after parsing is finished.
7. Convert station names to `String` only for final sorted output.

That shape matters more than any single trick. The fastest code is fast because the hot loop avoids general-purpose APIs.

## Topic 1: Memory-Mapped File Access

### Skill

Use memory mapping so the operating system pages the file into memory and the program can scan it as a contiguous byte range. This avoids repeated read calls and lets workers use pointer-like addresses.

### Common approaches

Most top entries use:

```java
FileChannel.map(FileChannel.MapMode.READ_ONLY, 0, fileSize, Arena.global())
```

Then they read from the mapped address with `Unsafe.getByte()` or `Unsafe.getLong()`.

### Why it helps

The parser can read 8 bytes at a time. For example, a station name can be scanned by loading a `long`, checking whether it contains `;`, and moving forward by 8 bytes if it does not.

### Solutions using it

`thomaswue`, `artsiomkorzun`, `jerrinot`, `serkan-ozal`, `abeobk`, `stephenvonworley`, `royvanrijn`, `yavuztas`, `mtopolnik`, `merykittyunsafe`, `gonixunsafe`, `yourwass`, `linl33`, and `tivrfoa` all use memory mapping with direct address access or foreign memory segments.

`gonix` uses `MappedByteBuffer`, which is safer and easier to understand than raw `Unsafe`, but a little slower.

## Topic 2: Chunking and Parallelism

### Skill

Split the file so all CPU cores can parse at the same time, while making sure chunks never split a row in half.

### Two main strategies

Equal static split:

Each worker gets one large region. This is simple and often good enough.

Dynamic work stealing:

Workers take the next chunk from an atomic counter or shared queue. This balances uneven chunk cost better because some chunks have longer station names or more hash collisions.

### Boundary handling

Because a raw byte offset may land in the middle of a line, solutions move the start or end offset to the next newline. This guarantees that each row is processed exactly once.

### Examples

`thomaswue` uses 2 MB segments and an `AtomicLong` cursor. Each worker repeatedly claims the next segment. Inside each segment it splits work into three sub-ranges, so one thread keeps multiple independent cursors moving.

`artsiomkorzun` uses 2 MB segments and an `AtomicInteger` segment counter.

`jerrinot` uses a global cursor and 4 MB segments.

`stephenvonworley` puts chunks in a `ConcurrentLinkedDeque`; workers poll until no chunks remain.

`serkan-ozal` creates 256 regions and lets a fixed thread pool consume tasks from a concurrent queue.

`mtopolnik`, `merykittyunsafe`, `yourwass`, `linl33`, `royvanrijn`, and `gonix` use more static splits.

`tivrfoa` uses a hybrid chunk plan: larger chunks for the first part of the file and smaller chunks later, with an atomic index for workers.

## Topic 3: Unsafe and Foreign Memory

### Skill

Read bytes and longs directly from memory with very little API overhead.

### Why top solutions use it

Normal Java array or buffer access includes safety checks. Those checks are valuable in production code, but in this challenge they cost time. `Unsafe.getLong(address)` can read eight bytes from a mapped file with less overhead.

### Tradeoff

This is not production-style Java. A wrong address can crash the JVM. Several solutions also depend on little-endian CPU behavior.

### Examples

`artsiomkorzun` stores the whole custom aggregation table off heap with `Unsafe.allocateMemory()`.

`jerrinot` has separate fast and slow maps backed by raw memory.

`gonixunsafe` stores an index and station records in manually allocated memory.

`linl33` uses Java 22 foreign memory plus native `malloc` and `calloc` through the foreign function API.

`gonix` is the useful contrast: it stays closer to normal Java by using `MappedByteBuffer`, `ByteBuffer`, `long[]`, and `int[]`.

## Topic 4: SWAR Byte Scanning

### Skill

Use one machine word to test several bytes at once. SWAR means "SIMD within a register". The code is scalar Java, but it uses bit operations as if one `long` contains eight small lanes.

### Delimiter detection

To find `;`, many solutions load eight bytes and compare them against:

```text
0x3B3B3B3B3B3B3B3B
```

The result tells whether any byte in the word is `;`. The same idea finds newline with:

```text
0x0A0A0A0A0A0A0A0A
```

### Why it helps

Instead of checking each byte with a branch, the code checks eight bytes with arithmetic and bit masks. That reduces branch misprediction and lets the CPU do more useful work per load.

### Examples

`thomaswue`, `jerrinot`, `abeobk`, `yavuztas`, `mtopolnik`, `gonixunsafe`, `gonix`, and `tivrfoa` rely heavily on this technique.

`thomaswue` and `stephenvonworley` also parse three cursors at once inside a chunk. This creates more independent work for the CPU and hides latency.

## Topic 5: Vector API

### Skill

Use `jdk.incubator.vector.ByteVector` to compare many bytes at once.

### Where it helps

Vector API is especially useful for:

1. Finding `;` or `\n`.
2. Comparing short station names.
3. Processing batches aligned to the vector size.

### Examples

`serkan-ozal` chooses 128-bit vectors because most station names are short enough that wider vectors did not help as much.

`merykittyunsafe` uses vectors to find semicolons and compare station names stored in the custom map.

`yourwass` uses vectors to find delimiters and compare city names when the name fits in one vector.

`linl33` uses vectors to find newline positions, then processes complete lines discovered from the vector mask.

### Important lesson

Vector API is not automatically faster. It works best after the data layout and parsing loop are already designed around byte-level scanning.

## Topic 6: Fixed-Point Temperature Parsing

### Skill

Parse temperatures as integer tenths instead of `double`.

For example:

```text
12.3  -> 123
-4.5  -> -45
```

Only the final output converts back to one decimal place.

### Why it helps

The input format is fixed and small. A temperature is always one decimal digit, and the valid range is limited. Integer math is faster, simpler to aggregate, and avoids floating-point parsing.

### Branchless parser

Many entries use a parser popularized by merykitty. It loads the temperature bytes into one `long`, finds the decimal point, removes the sign, extracts digit nibbles, multiplies by a constant, and applies the sign.

The high-level idea:

1. Locate `.` from bit patterns.
2. Align the digit bytes.
3. Keep only the low digit nibbles.
4. Combine hundreds, tens, and ones with one multiplication.
5. Apply the sign without branching.

### Examples

`merykittyunsafe` contains the core version and explains it in comments.

`abeobk`, `yavuztas`, `tivrfoa`, `gonix`, and `gonixunsafe` use very similar branchless parsing.

`yourwass` uses lookup tables instead. It precomputes decimal and fractional pieces and then parses temperatures with table reads.

`linl33` parses from the end of each line by reading the bytes before `\n`, which works because the temperature format is small and predictable.

## Topic 7: Custom Hash Tables

### Skill

Replace `HashMap<String, Stats>` with a specialized table for at most 10,000 station names.

### Why normal `HashMap` is slow here

`HashMap<String, Stats>` requires building `String` keys, allocating objects, computing general-purpose string hashes, following object pointers, and handling resizing logic. The top solutions avoid most of this.

### Common table design

Most custom tables use:

1. Power-of-two capacity.
2. Masking instead of modulo.
3. Open addressing or linear probing.
4. Primitive fields for `min`, `max`, `sum`, and `count`.
5. Raw station-name bytes or address references.

### Examples

`artsiomkorzun` uses a 64K-entry off-heap table where each entry is 128 bytes.

`jerrinot` uses fast and slow maps. Short names can be matched by one or two `long` words; longer names use a slower path.

`abeobk` uses a `Node[]` table with linear probing and stores hash, first word, key address, key length, and stats.

`mtopolnik` uses an off-heap `StatsAccessor` table with cache-line-aware allocation.

`merykittyunsafe` uses a `byte[]`-backed open-address table with 128-byte entries.

`gonix` uses `int[]` as an index and `long[]` as compact record storage.

`gonixunsafe` moves the same idea off heap.

`linl33` uses a sparse table for lookup and a dense table for iterating real entries quickly.

## Topic 8: Station Name Handling

### Skill

Delay `String` creation until the final output.

### Why it helps

There can be one billion rows but only up to 10,000 unique station names. Creating a `String` per row would be catastrophic. Top solutions compare names as bytes or longs while parsing, and only create strings for unique station names during final sorting.

### Fast name matching

Short names are common, so many solutions optimize the first 8 or 16 bytes:

1. Load first word.
2. Check for `;`.
3. If found, mask away bytes after the delimiter.
4. Use the word as part of the hash and equality check.

Longer names fall back to comparing additional 8-byte words.

### Examples

`thomaswue` stores first and second name words for the fast path and uses a slower path for names longer than 16 bytes.

`jerrinot` has a fast map for short names and a slow map for longer names.

`yavuztas` stores first word, second word, last word, length, and original memory address.

`yourwass` stores city address and length, then compares vector-sized chunks when possible.

`linl33` stores the original name address and length in the hash table.

## Topic 9: Worker-Local Aggregation and Final Merge

### Skill

Avoid shared writes in the hot loop.

### Why it helps

If every row updated one global concurrent map, the program would lose time to locking, cache-line contention, and memory fences. Top solutions let each worker update its own map, then merge after parsing.

### Merge strategies

Most solutions merge into a final `TreeMap` or sort a compact list of entries at the end.

`gonixunsafe` is unusual: workers merge `Aggregator` instances through an `AtomicReference`, reducing the number of final merge steps while still keeping parsing local.

`linl33` starts merging worker maps into map 0 with `CompletableFuture.runAfterBothAsync()`.

`yourwass` uses a lock while each thread contributes its local results to a shared `TreeMap`; this lock is outside the per-row hot path.

## Topic 10: Native Image and JVM Tuning

### Skill

After the algorithm is tight, use runtime configuration to remove startup and runtime overhead.

### GraalVM native image

Several fastest solutions can run as native images. Their launcher scripts first check for an image in `target/`, then fall back to JVM mode.

Native image helps because this challenge measures full process time, not only steady-state parsing time.

### Worker subprocess trick

Some solutions spawn a child process to do the work and pipe the output back. The parent can return after receiving output instead of waiting for memory unmapping cleanup.

Used by: `thomaswue`, `artsiomkorzun`, `jerrinot`, `abeobk`, `stephenvonworley`, `royvanrijn`, `yavuztas`, and `mtopolnik`.

### JVM flags

The launchers use flags such as:

1. `--enable-preview`
2. `--add-modules=jdk.incubator.vector`
3. `--enable-native-access=ALL-UNNAMED`
4. `-XX:-TieredCompilation`
5. `-XX:+UseNUMA`
6. `-XX:+UseTransparentHugePages`
7. `-Djdk.incubator.vector.VECTOR_ACCESS_OOB_CHECK=0`

These are finishing touches. They cannot save a slow parser, but they can matter once the core loop is already close to hardware limits.

## Solution Profiles

### thomaswue

Main ideas:

1. GraalVM native-image support.
2. Worker subprocess to avoid memory-unmap delay.
3. Memory-mapped file with foreign memory address access.
4. Dynamic work stealing with 2 MB segments.
5. Three cursors per segment.
6. Custom hash table.
7. SWAR delimiter detection.
8. Branchless integer temperature parsing.

This is the fastest listed solution. The important design choice is that it does not merely split the file by core. It uses many small segments and lets workers claim more work dynamically. That reduces load imbalance. The three-cursor parser also keeps the CPU supplied with independent operations.

Good to study for: overall architecture, dynamic chunking, subprocess trick, and extreme hot-loop design.

### artsiomkorzun

Main ideas:

1. Memory mapping through `MemorySegment`.
2. `Unsafe` reads from mapped memory.
3. 2 MB segment work stealing with an atomic counter.
4. Off-heap aggregation table with 128-byte entries.
5. Fast word-based delimiter detection.
6. Native-image preparation with epsilon GC and aggressive optimization flags.

The standout feature is the off-heap `Aggregates` table. It stores station metadata and stats in raw memory instead of Java objects. That improves locality and removes object allocation from the parser.

Good to study for: raw memory table layout and worker-local off-heap aggregation.

### jerrinot

Main ideas:

1. Worker subprocess.
2. Memory mapping plus `Unsafe`.
3. Global cursor over 4 MB segments.
4. Separate fast and slow station maps.
5. Branchless parser borrowed from merykitty.
6. Hashing ideas borrowed from mtopolnik.
7. Lookup-table masks borrowed from abeobk.

This solution is valuable because it combines many winning ideas explicitly. Short station names are handled in a compact fast path, while longer names go to a slower path. That is a common high-performance technique: make the common case extremely fast and keep the rare case correct.

Good to study for: fast-path/slow-path design and practical composition of known optimizations.

### serkan-ozal

Main ideas:

1. Vector API for delimiter search and key comparison.
2. Shared memory region option.
3. 256 regions consumed by a thread pool.
4. Configurable platform threads or virtual threads.
5. Custom byte-array-backed result map.
6. Heavy JVM tuning in the launcher script.

The solution uses 128-bit byte vectors because experiments showed most station names fit well in that size. This is a useful reminder that wider vectors are not always faster.

Good to study for: Java Vector API, task queue design, and runtime tuning.

### abeobk

Main ideas:

1. Worker subprocess.
2. Memory mapping and `Unsafe`.
3. 4 MB chunks.
4. Each worker divides a chunk into three parsers.
5. SWAR helpers for semicolon, newline, and decimal point detection.
6. Custom `Node[]` hash table with linear probing.
7. Branchless temperature parsing.

`abeobk` is compact and clear compared with some other top entries. The helper functions show the core byte-scanning tricks directly: find delimiter, find newline, find dot, parse number, mix hash.

Good to study for: readable versions of SWAR helpers and chunk parsing.

### stephenvonworley

Main ideas:

1. Worker subprocess.
2. Memory mapping.
3. Queue of chunks.
4. One table per worker.
5. Three-way parsing inside chunks.
6. Off-heap table layout.
7. Native-image support.

This solution documents its pipeline clearly in comments. The `parse3()` strategy is similar in spirit to `thomaswue`: one thread processes three independent positions in the chunk to improve instruction-level parallelism.

Good to study for: well-described architecture and three-cursor parsing.

### royvanrijn

Main ideas:

1. Worker subprocess.
2. Memory mapping plus `Unsafe`.
3. Segment per processor.
4. Flyweight `byte[]` entries.
5. Concurrent final merge with `ConcurrentHashMap`.
6. Delayed string creation.
7. Extensive changelog showing optimization steps.

This file is useful because the comments show the path from a slow implementation to a fast one. It records improvements such as skipping string creation, adding memory mapping, using a custom map, using SWAR token checks, and improving layout.

Good to study for: performance iteration and understanding which optimizations mattered.

### yavuztas

Main ideas:

1. Worker subprocess.
2. `Unsafe` over a memory-mapped file.
3. Twice as many regions as processors for better balancing.
4. Custom `RecordMap`.
5. Records store original address, length, first words, last word, hash, and stats.
6. Linked-list collision handling.
7. Branchless temperature parsing.

The solution keeps one `Record` object per unique station per worker, not per row. That is still very low allocation compared with normal parsing. Its name equality checks compare longs instead of bytes.

Good to study for: a slightly more object-oriented custom map that remains fast.

### mtopolnik

Main ideas:

1. Worker subprocess.
2. Static chunk split by processor count.
3. Memory-mapped file with `Unsafe` reads.
4. Off-heap stats table allocated per worker.
5. Short-name fast paths for names within one or two words.
6. Merge-sort-style final output from sorted worker results.

The parser specializes names of length up to 8 and 16 bytes, then falls back for longer names. The table stores station names in off-heap slots and compares by words.

Good to study for: short-name specialization and cache-aware table design.

### merykittyunsafe

Main ideas:

1. Memory mapping.
2. `Unsafe` and Java Vector API.
3. Worker-local `PoorManMap`.
4. 128-byte entries in a `byte[]`.
5. Vector delimiter search and vector key comparison.
6. Branchless fixed-point temperature parser.
7. Simple scalar fallback for tails.

This is one of the best files for learning the famous branchless temperature parser because the comments explain the digit packing and multiplication. It also shows a clean custom map design using a plain byte array.

Good to study for: temperature parsing and vector-assisted map lookup.

### gonixunsafe

Main ideas:

1. Memory mapping.
2. Off-heap memory allocation with `Unsafe`.
3. Chunks aligned to newline boundaries.
4. Poor-man hash map with a separate index and contiguous record storage.
5. Padded tail handling to avoid unsafe over-read near the end of a mapping.
6. AtomicReference-based aggregator merging.

This is the unsafe version of the `gonix` design. It keeps the same compact table idea but moves storage off heap and uses raw address reads.

Good to study for: compact data layout, tail safety, and off-heap conversion of a buffer-based solution.

### yourwass

Main ideas:

1. Memory mapping and `Unsafe`.
2. Vector API for delimiter search and short-name comparison.
3. Per-thread raw memory result area.
4. Lookup tables for temperature parsing.
5. Station keys stored as address plus length.
6. Final merge into a `TreeMap` under a lock.

The unusual part is temperature parsing through precomputed lookup tables rather than the branchless multiplication parser. This trades arithmetic for indexed table reads.

Good to study for: lookup-table parsing and vectorized short-key matching.

### linl33

Main ideas:

1. Java 22 features.
2. Memory mapping through foreign memory.
3. Vector API to find newline positions.
4. Thread pool with platform threads.
5. Asynchronous merge into the first table.
6. Off-heap sparse/dense hash table.
7. Native `malloc`/`calloc` through foreign function calls.

Unlike many parsers that scan from the station name forward to `;`, this one uses vector scanning to find line endings, then parses the known fixed-size temperature tail. The hash table stores raw addresses and lengths.

Good to study for: newline-first parsing and sparse/dense table layout.

### tivrfoa

Main ideas:

1. Memory mapping with `Unsafe`.
2. Work distribution using precomputed chunk sizes and an atomic chunk index.
3. Custom bucket table.
4. XXH3 avalanche-style hash mixing.
5. Branchless temperature parser.
6. Final merge into a `TreeMap`.

This solution is explicitly based on an earlier `thomaswue` version and experiments with hashing and chunk distribution. It is useful for studying collision behavior with 10,000 unique station names.

Good to study for: hash quality tradeoffs and chunk scheduling experiments.

### gonix

Main ideas:

1. `MappedByteBuffer` instead of raw address access.
2. Parallel stream over file chunks.
3. Compact `int[]` index plus `long[]` record storage.
4. SWAR delimiter and decimal detection.
5. Fixed-point integer temperature parsing.
6. Tail copy into padded buffer for safe long reads.

This is the most approachable top solution because it avoids much of the raw `Unsafe` style. It still uses the same important ideas: chunking, long-at-a-time parsing, fixed-point temperatures, and a custom primitive table.

Good to study for: learning the core algorithm before moving to `Unsafe`.

## Recommended Learning Order

1. Start with `gonix` to understand chunking, SWAR parsing, fixed-point temperatures, and compact tables without raw pointers.
2. Read `merykittyunsafe` for the temperature parser and vector-assisted key matching.
3. Read `abeobk` for concise SWAR helpers and worker chunk parsing.
4. Read `mtopolnik` or `yavuztas` for custom table design and name equality.
5. Read `thomaswue`, `artsiomkorzun`, and `jerrinot` for the final layer of extreme optimization.
6. Read launcher and prepare scripts after the algorithm is clear; runtime flags are the last layer, not the foundation.

## Practical Takeaways

The reusable skills are:

1. Design the data path before tuning the runtime.
2. Avoid allocation in the hot loop.
3. Parse structured text as bytes when the format is fixed.
4. Use fixed-point integers for decimal values with known precision.
5. Keep per-thread state local.
6. Merge after parallel parsing.
7. Create strings only when they are truly needed.
8. Use specialized hash tables when the input limits are known.
9. Use vector/SWAR techniques to reduce branch-heavy byte scanning.
10. Apply native image and JVM flags only after the algorithm is already efficient.

