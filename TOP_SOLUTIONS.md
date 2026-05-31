# Top 1BRC Solutions Study Guide

This repository has been trimmed to the main 1B-row leaderboard entries that finished in less than three seconds on the official evaluation machine. Lower and middle tier submissions were removed so the remaining tree is easier to study.

| Result | Launcher | Source |
| --- | --- | --- |
| 00:01.535 | `calculate_average_thomaswue.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_thomaswue.java` |
| 00:01.587 | `calculate_average_artsiomkorzun.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_artsiomkorzun.java` |
| 00:01.608 | `calculate_average_jerrinot.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_jerrinot.java` |
| 00:01.880 | `calculate_average_serkan-ozal.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_serkan_ozal.java` |
| 00:01.921 | `calculate_average_abeobk.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_abeobk.java` |
| 00:02.018 | `calculate_average_stephenvonworley.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_stephenvonworley.java` |
| 00:02.157 | `calculate_average_royvanrijn.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_royvanrijn.java` |
| 00:02.319 | `calculate_average_yavuztas.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_yavuztas.java` |
| 00:02.332 | `calculate_average_mtopolnik.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_mtopolnik.java` |
| 00:02.367 | `calculate_average_merykittyunsafe.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_merykittyunsafe.java` |
| 00:02.507 | `calculate_average_gonixunsafe.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_gonixunsafe.java` |
| 00:02.557 | `calculate_average_yourwass.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_yourwass.java` |
| 00:02.820 | `calculate_average_linl33.sh` | `src/main/java-22/dev/morling/onebrc/CalculateAverage_linl33.java` |
| 00:02.995 | `calculate_average_tivrfoa.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_tivrfoa.java` |
| 00:02.997 | `calculate_average_gonix.sh` | `src/main/java/dev/morling/onebrc/CalculateAverage_gonix.java` |

Study these recurring techniques first:

1. Partition the mapped file by byte ranges, then adjust each worker range to line boundaries.
2. Avoid object allocation in the hot path. Most top entries aggregate with custom hash tables and primitive fields.
3. Parse temperatures as fixed-point integers instead of floating-point values.
4. Use station-name byte fingerprints or custom equality checks to avoid eagerly creating strings.
5. Merge per-worker maps only after parsing, keeping the write-heavy path local to each worker.
6. Use `Unsafe`, foreign memory access, or `MappedByteBuffer` to reduce bounds checks and copying.
7. Use GraalVM native image, CDS, JVM flags, and Vector API only after the parsing and aggregation path is already tight.

Useful commands:

```bash
./mvnw --quiet -Dquick verify
./test.sh thomaswue
./test.sh artsiomkorzun
./evaluate.sh thomaswue artsiomkorzun jerrinot
```
