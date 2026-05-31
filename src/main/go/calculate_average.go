package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"math/bits"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	tableSize   = 1 << 15
	chunkFactor = 32

	semicolon64 = 0x3b3b3b3b3b3b3b3b
	loBits64    = 0x0101010101010101
	hiBits64    = 0x8080808080808080
)

type chunk struct {
	start int
	end   int
}

type entry struct {
	hash  uint64
	off   int
	sum   int64
	count uint64
	min   int16
	max   int16
	len   uint16
}

type result struct {
	off   int
	len   int
	min   int16
	max   int16
	sum   int64
	count uint64
}

func main() {
	path := "measurements.txt"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	data, err := mmapFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(data) == 0 {
		fmt.Println("{}")
		return
	}

	workers := workerCount()
	chunks := buildChunks(data, workers*chunkFactor)
	if len(chunks) < workers {
		workers = len(chunks)
	}

	tables := make([][]entry, workers)
	var next atomic.Uint64
	var wg sync.WaitGroup
	wg.Add(workers)
	for id := 0; id < workers; id++ {
		go func(id int) {
			defer wg.Done()
			table := make([]entry, tableSize)
			for {
				i := int(next.Add(1)) - 1
				if i >= len(chunks) {
					break
				}
				c := chunks[i]
				processChunk(data, c.start, c.end, table)
			}
			tables[id] = table
		}(id)
	}
	wg.Wait()

	finalTable := make([]entry, tableSize)
	for _, table := range tables {
		for i := range table {
			e := &table[i]
			if e.count != 0 {
				mergeEntry(data, finalTable, e)
			}
		}
	}

	results := collectResults(finalTable)
	sort.Slice(results, func(i, j int) bool {
		a := results[i]
		b := results[j]
		return bytes.Compare(data[a.off:a.off+a.len], data[b.off:b.off+b.len]) < 0
	})

	writeOutput(data, results)
}

func mmapFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}
	if int64(int(size)) != size {
		return nil, fmt.Errorf("file too large for this platform: %d bytes", size)
	}

	data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}

	// Do not munmap explicitly. This process is a short-lived batch program and
	// the OS reclaims the mapping at exit; unmapping before exit only adds timed work.
	return data, nil
}

func workerCount() int {
	if raw := os.Getenv("BRC_WORKERS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		return 1
	}
	return n
}

func buildChunks(data []byte, wanted int) []chunk {
	n := len(data)
	if wanted < 1 {
		wanted = 1
	}
	if wanted > n {
		wanted = n
	}

	size := (n + wanted - 1) / wanted
	chunks := make([]chunk, 0, wanted)
	start := 0
	for start < n {
		end := start + size
		if end >= n {
			end = n
		}
		for end < n && data[end-1] != '\n' {
			end++
		}
		if end > start {
			chunks = append(chunks, chunk{start: start, end: end})
		}
		start = end
	}
	return chunks
}

func processChunk(data []byte, start, end int, table []entry) {
	pos := start
	for pos < end {
		nameStart := pos
		semi := findSemicolon(data, pos, end)
		hash := hashBytes(data, nameStart, semi)
		nameLen := semi - nameStart
		pos = semi + 1

		value := parseTemperature(data, &pos)
		updateEntry(data, table, hash, nameStart, nameLen, value)
	}
}

func findSemicolon(data []byte, pos, end int) int {
	i := pos
	limit := end - 8
	for i <= limit {
		w := load64(data, i) ^ semicolon64
		mask := (w - loBits64) &^ w & hiBits64
		if mask != 0 {
			return i + bits.TrailingZeros64(mask)/8
		}
		i += 8
	}
	for data[i] != ';' {
		i++
	}
	return i
}

func parseTemperature(data []byte, pos *int) int16 {
	i := *pos
	neg := false
	if data[i] == '-' {
		neg = true
		i++
	}

	first := int(data[i] - '0')
	var value int
	if data[i+1] == '.' {
		value = first*10 + int(data[i+2]-'0')
		i += 3
	} else {
		value = first*100 + int(data[i+1]-'0')*10 + int(data[i+3]-'0')
		i += 4
	}

	if neg {
		value = -value
	}
	if i < len(data) && data[i] == '\n' {
		i++
	}
	*pos = i
	return int16(value)
}

func updateEntry(data []byte, table []entry, hash uint64, off, nameLen int, value int16) {
	mask := len(table) - 1
	idx := int(hash) & mask
	for {
		e := &table[idx]
		if e.count == 0 {
			e.hash = hash
			e.off = off
			e.len = uint16(nameLen)
			e.min = value
			e.max = value
			e.sum = int64(value)
			e.count = 1
			return
		}
		if e.hash == hash && int(e.len) == nameLen && bytes.Equal(data[e.off:e.off+int(e.len)], data[off:off+nameLen]) {
			if value < e.min {
				e.min = value
			}
			if value > e.max {
				e.max = value
			}
			e.sum += int64(value)
			e.count++
			return
		}
		idx = (idx + 1) & mask
	}
}

func mergeEntry(data []byte, table []entry, src *entry) {
	mask := len(table) - 1
	idx := int(src.hash) & mask
	srcLen := int(src.len)
	for {
		dst := &table[idx]
		if dst.count == 0 {
			*dst = *src
			return
		}
		if dst.hash == src.hash && int(dst.len) == srcLen && bytes.Equal(data[dst.off:dst.off+int(dst.len)], data[src.off:src.off+srcLen]) {
			if src.min < dst.min {
				dst.min = src.min
			}
			if src.max > dst.max {
				dst.max = src.max
			}
			dst.sum += src.sum
			dst.count += src.count
			return
		}
		idx = (idx + 1) & mask
	}
}

func collectResults(table []entry) []result {
	results := make([]result, 0, 10000)
	for i := range table {
		e := &table[i]
		if e.count != 0 {
			results = append(results, result{
				off:   e.off,
				len:   int(e.len),
				min:   e.min,
				max:   e.max,
				sum:   e.sum,
				count: e.count,
			})
		}
	}
	return results
}

func writeOutput(data []byte, results []result) {
	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	out.WriteByte('{')
	for i, r := range results {
		if i > 0 {
			out.WriteString(", ")
		}
		out.Write(data[r.off : r.off+r.len])
		out.WriteByte('=')
		writeTenths(out, int64(r.min))
		out.WriteByte('/')
		writeTenths(out, javaRound(float64(r.sum)/float64(r.count)))
		out.WriteByte('/')
		writeTenths(out, int64(r.max))
	}
	out.WriteString("}\n")
	out.Flush()
}

func writeTenths(out *bufio.Writer, value int64) {
	if value < 0 {
		out.WriteByte('-')
		value = -value
	}
	out.WriteString(strconv.FormatInt(value/10, 10))
	out.WriteByte('.')
	out.WriteByte(byte('0' + value%10))
}

func javaRound(v float64) int64 {
	return int64(math.Floor(v + 0.5))
}

func hashBytes(data []byte, start, end int) uint64 {
	h := uint64(end-start) ^ 0x9e3779b97f4a7c15
	i := start
	for i+8 <= end {
		h ^= mix64(load64(data, i))
		h = bits.RotateLeft64(h, 27)*0x3c79ac492ba7b653 + 0x1c69b3f74ac4ae35
		i += 8
	}

	var tail uint64
	shift := uint(0)
	for i < end {
		tail |= uint64(data[i]) << shift
		shift += 8
		i++
	}
	h ^= mix64(tail ^ uint64(end-start)*0x165667b19e3779f9)
	return mix64(h)
}

func mix64(x uint64) uint64 {
	x ^= x >> 32
	x *= 0xd6e8feb86659fd93
	x ^= x >> 32
	x *= 0xd6e8feb86659fd93
	x ^= x >> 32
	return x
}

func load64(data []byte, i int) uint64 {
	return *(*uint64)(unsafe.Pointer(&data[i]))
}
