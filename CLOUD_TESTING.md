# Cloud Testing Guide for 1BRC

This guide chooses simple instance types for testing the top 1BRC solutions on AWS and GCP.

The goal is not to reproduce the official leaderboard exactly. The goal is to run the algorithms in a cloud VM and see the same performance patterns: memory mapping, parallel chunk parsing, custom hash tables, fixed-point parsing, and final merge.

## Official Baseline Hardware

The original official 1BRC evaluation machine was:

```text
Hetzner AX161
CPU: AMD EPYC 7502P, 32 cores / 64 threads, Zen2, 2.5 GHz
RAM: 128 GB ECC DDR4
Main leaderboard run: 8 cores
SMT: off
Turbo Boost: off
Input: RAM disk
```

Important: the machine had 32 cores, but the main leaderboard used 8 cores. If you use more or fewer cores, your result is useful for learning but not directly comparable.

## AWS Recommendation

### Closest To Official Hardware: `m8a.8xlarge`

Use this if you want a cloud machine close to the original hardware shape.

```text
Instance: m8a.8xlarge
CPU: AMD EPYC 9R45
vCPU: 32
Memory: 128 GiB
Storage: EBS-only
```

Why this is the closest AWS choice:

1. Same broad CPU vendor family: AMD EPYC.
2. Same memory size as the official server: 128 GB.
3. Same visible CPU count shape: 32 vCPU.
4. AWS M8a vCPUs are listed as one thread per core in current instance specs, so this is closer to the official "SMT off" setup than many older EC2 shapes.

Use it for:

1. Full 1B-row tests.
2. RAM-disk experiments.
3. Comparing multiple top solutions seriously.

Suggested setup:

```bash
sudo mkdir -p /mnt/ramdisk
sudo mount -t tmpfs -o size=80G tmpfs /mnt/ramdisk
```

Generate or copy `measurements.txt` into the RAM disk, then run tests from there.

To imitate the official 8-core run:

```bash
taskset -c 0-7 ./evaluate.sh thomaswue artsiomkorzun jerrinot
```

### Budget Sanity Test: `m8a.xlarge`

Use this if you only want to confirm the algorithms work and compare optimized vs naive behavior.

```text
Instance: m8a.xlarge
CPU: AMD EPYC 9R45
vCPU: 4
Memory: 16 GiB
Storage: EBS-only
```

Why this is the budget AWS choice:

1. It stays on AMD EPYC.
2. It has enough memory for small and medium test files.
3. It is cheaper than 8-core and full-size benchmark instances.

Use it for:

1. 10M-row tests.
2. 50M-row tests.
3. Correctness checks.
4. Learning how the top algorithms scale.

Avoid full 1B-row RAM-disk testing on this instance. Use smaller data:

```bash
./create_measurements.sh 10000000
./test.sh gonix
./test.sh merykittyunsafe
```

## GCP Recommendation

### Closest To Official Hardware: `c3d-standard-30`

Use this if you want a GCP machine close to the original AMD EPYC server shape.

```text
Machine type: c3d-standard-30
CPU: 4th generation AMD EPYC Genoa
vCPU: 30
Memory: 120 GB
Storage: Persistent Disk by default
```

Why this is the closest GCP choice:

1. AMD EPYC CPU family.
2. 30 vCPU is close to the original 32-core server.
3. 120 GB memory is close to the original 128 GB.
4. C3D is designed for consistent high performance.

Use it for:

1. Full 1B-row tests.
2. RAM-disk experiments if memory headroom is enough.
3. Cross-cloud comparison against AWS `m8a.8xlarge`.

If you want local SSD, use:

```text
c3d-standard-30-lssd
```

That gives local SSD capacity for staging data, but RAM disk is still better for isolating algorithm performance from storage performance.

To imitate the official 8-core run:

```bash
taskset -c 0-7 ./evaluate.sh thomaswue artsiomkorzun jerrinot
```

### Budget Sanity Test: `c3d-standard-4`

Use this if you want a cheap GCP machine that still has enough memory to run meaningful small tests.

```text
Machine type: c3d-standard-4
CPU: 4th generation AMD EPYC Genoa
vCPU: 4
Memory: 16 GB
Storage: Persistent Disk
```

Why this is the budget GCP choice:

1. It keeps the same C3D AMD family.
2. 16 GB is more comfortable than highcpu 4-vCPU shapes with only 8 GB.
3. It is enough to confirm the top algorithms work as expected.

Use it for:

1. 10M-row tests.
2. 50M-row tests.
3. Correctness and relative algorithm comparison.

Avoid full 1B-row RAM-disk testing on this machine.

## Suggested Test Sizes

For budget instances:

```bash
./create_measurements.sh 10000000      # 10M rows
./create_measurements.sh 50000000      # 50M rows
```

For close-to-official instances:

```bash
./create_measurements.sh 100000000     # 100M rows warm-up scale
./create_measurements.sh 1000000000    # full 1B rows
```

## Recommended Test Flow

Start with a simple solution:

```bash
./test.sh gonix
```

Then test unsafe/vector-heavy solutions:

```bash
./test.sh merykittyunsafe
./test.sh abeobk
./test.sh thomaswue
```

Then compare the top set:

```bash
./evaluate.sh thomaswue artsiomkorzun jerrinot serkan-ozal abeobk
```

## Notes

1. Do not compare cloud results directly with official results unless core count, CPU behavior, storage, OS, Java version, and data location are controlled.
2. Use RAM disk when possible; otherwise storage can dominate.
3. Avoid burstable instances such as AWS `t*`.
4. Avoid ARM instances for first testing. Some solutions assume x86 behavior or are tuned for x86/GraalVM paths.
5. Budget instances are for learning and correctness, not leaderboard-like timing.

## Sources

AWS M8a:

https://aws.amazon.com/ec2/instance-types/m8a/

AWS compute optimized specs:

https://docs.aws.amazon.com/ec2/latest/instancetypes/co.html

GCP C3D:

https://docs.cloud.google.com/compute/docs/general-purpose-machines#c3d_machine_series

