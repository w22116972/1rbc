package main

import (
	"bufio"
	"bytes"
	"fmt"
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
	tableSlots  = 1 << 16
	chunkFactor = 32

	semicolonWord = 0x3b3b3b3b3b3b3b3b
	loBits        = 0x0101010101010101
	hiBits        = 0x8080808080808080

	hashSeed uint64 = 0x9e3779b97f4a7c15
	hashMul1 uint64 = 0x3c79ac492ba7b653
	hashMul2 uint64 = 0x1c69b3f74ac4ae35
)

type chunk struct {
	start int
	end   int
}

type stationEntry struct {
	hash  uint64
	p1    uint64
	p2    uint64
	last  uint64
	sum   int64
	count uint64
	off   int
	min   int16
	max   int16
	len   uint16
}

type outputEntry struct {
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

	data, err := mmapInput(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(data) == 0 {
		fmt.Println("{}")
		return
	}

	workers := configuredWorkers()
	chunks := splitChunks(data, workers*chunkFactor)
	if len(chunks) < workers {
		workers = len(chunks)
	}

	tables := make([][]stationEntry, workers)
	var cursor atomic.Uint64
	var wg sync.WaitGroup
	wg.Add(workers)
	for workerID := 0; workerID < workers; workerID++ {
		go func(workerID int) {
			defer wg.Done()
			table := make([]stationEntry, tableSlots)
			for {
				chunkID := int(cursor.Add(1)) - 1
				if chunkID >= len(chunks) {
					break
				}
				c := chunks[chunkID]
				parseChunk(data, c.start, c.end, table)
			}
			tables[workerID] = table
		}(workerID)
	}
	wg.Wait()

	finalTable := make([]stationEntry, tableSlots)
	for _, table := range tables {
		for i := range table {
			if table[i].count != 0 {
				mergeStation(data, finalTable, &table[i])
			}
		}
	}

	results := collect(finalTable)
	sort.Slice(results, func(i, j int) bool {
		a := results[i]
		b := results[j]
		return bytes.Compare(data[a.off:a.off+a.len], data[b.off:b.off+b.len]) < 0
	})

	writeResults(data, results)
}

func mmapInput(path string) ([]byte, error) {
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
	adviseSequential(data)
	return data, nil
}

func configuredWorkers() int {
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

func splitChunks(data []byte, wanted int) []chunk {
	size := len(data)
	if wanted < 1 {
		wanted = 1
	}
	if wanted > size {
		wanted = size
	}

	target := (size + wanted - 1) / wanted
	chunks := make([]chunk, 0, wanted)
	start := 0
	for start < size {
		end := start + target
		if end >= size {
			end = size
		} else {
			for data[end-1] != '\n' {
				end++
			}
		}
		chunks = append(chunks, chunk{start: start, end: end})
		start = end
	}
	return chunks
}

func parseChunk(data []byte, start, end int, table []stationEntry) {
	pos := start
	for pos < end {
		nameStart := pos
		semi, hash, p1, p2, last := scanName(data, nameStart, end)
		nameLen := semi - nameStart
		pos = semi + 1

		value, next := parseTemperature(data, pos)
		pos = next
		addMeasurement(data, table, hash, p1, p2, last, nameStart, nameLen, value)
	}
}

func scanName(data []byte, start, end int) (semi int, hash uint64, p1 uint64, p2 uint64, last uint64) {
	h := hashSeed
	i := start
	limit := end - 8
	for i <= limit {
		word := load64(data, i)
		marker := (word ^ semicolonWord)
		mask := (marker - loBits) &^ marker & hiBits
		if mask != 0 {
			n := bits.TrailingZeros64(mask) / 8
			if n != 0 {
				h = hashWord(h, word&byteMask(n))
			}
			semi = i + n
			nameLen := semi - start
			h ^= uint64(nameLen) * 0x165667b19e3779f9
			hash = finalizeHash(h)
			p1, p2, last = fingerprints(data, start, nameLen)
			return semi, hash, p1, p2, last
		}
		h = hashWord(h, word)
		i += 8
	}

	semi = i
	var tail uint64
	shift := uint(0)
	for data[semi] != ';' {
		tail |= uint64(data[semi]) << shift
		shift += 8
		semi++
	}
	if shift != 0 {
		h = hashWord(h, tail)
	}
	nameLen := semi - start
	h ^= uint64(nameLen) * 0x165667b19e3779f9
	hash = finalizeHash(h)
	p1, p2, last = fingerprints(data, start, nameLen)
	return semi, hash, p1, p2, last
}

func parseTemperature(data []byte, pos int) (int16, int) {
	i := pos
	neg := false
	if data[i] == '-' {
		neg = true
		i++
	}

	d0 := int(data[i] - '0')
	var value int
	if data[i+1] == '.' {
		value = d0*10 + int(data[i+2]-'0')
		i += 3
	} else {
		value = d0*100 + int(data[i+1]-'0')*10 + int(data[i+3]-'0')
		i += 4
	}
	if neg {
		value = -value
	}
	if i < len(data) && data[i] == '\n' {
		i++
	}
	return int16(value), i
}

func addMeasurement(data []byte, table []stationEntry, hash, p1, p2, last uint64, off, nameLen int, value int16) {
	mask := len(table) - 1
	idx := int(hash) & mask
	for {
		e := &table[idx]
		if e.count == 0 {
			e.hash = hash
			e.p1 = p1
			e.p2 = p2
			e.last = last
			e.off = off
			e.len = uint16(nameLen)
			e.min = value
			e.max = value
			e.sum = int64(value)
			e.count = 1
			return
		}
		if stationMatches(data, e, hash, p1, p2, last, off, nameLen) {
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

func mergeStation(data []byte, table []stationEntry, src *stationEntry) {
	mask := len(table) - 1
	idx := int(src.hash) & mask
	srcLen := int(src.len)
	for {
		dst := &table[idx]
		if dst.count == 0 {
			*dst = *src
			return
		}
		if stationMatches(data, dst, src.hash, src.p1, src.p2, src.last, src.off, srcLen) {
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

func stationMatches(data []byte, e *stationEntry, hash, p1, p2, last uint64, off, nameLen int) bool {
	if e.hash != hash || int(e.len) != nameLen || e.p1 != p1 || e.p2 != p2 || e.last != last {
		return false
	}
	if nameLen <= 24 {
		return true
	}
	return bytes.Equal(data[e.off:e.off+nameLen], data[off:off+nameLen])
}

func collect(table []stationEntry) []outputEntry {
	results := make([]outputEntry, 0, 10000)
	for i := range table {
		e := &table[i]
		if e.count != 0 {
			results = append(results, outputEntry{
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

func writeResults(data []byte, results []outputEntry) {
	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	out.WriteByte('{')
	for i, r := range results {
		if i != 0 {
			out.WriteString(", ")
		}
		out.Write(data[r.off : r.off+r.len])
		out.WriteByte('=')
		writeTenths(out, int64(r.min))
		out.WriteByte('/')
		writeTenths(out, roundJavaAverage(r.sum, r.count))
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

func roundJavaAverage(sum int64, count uint64) int64 {
	denominator := int64(count) * 2
	numerator := sum*2 + int64(count)
	quotient := numerator / denominator
	if numerator < 0 && numerator%denominator != 0 {
		quotient--
	}
	return quotient
}

func fingerprints(data []byte, off, nameLen int) (uint64, uint64, uint64) {
	p1 := loadPartial(data, off, minInt(nameLen, 8))
	if nameLen <= 8 {
		return p1, 0, p1
	}

	p2 := loadPartial(data, off+8, minInt(nameLen-8, 8))
	if nameLen <= 16 {
		return p1, p2, p2
	}

	last := load64(data, off+nameLen-8)
	return p1, p2, last
}

func hashWord(h, word uint64) uint64 {
	h ^= word
	return bits.RotateLeft64(h, 27)*hashMul1 + hashMul2
}

func finalizeHash(h uint64) uint64 {
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

func byteMask(n int) uint64 {
	if n >= 8 {
		return ^uint64(0)
	}
	return (uint64(1) << (uint(n) * 8)) - 1
}

func loadPartial(data []byte, off, n int) uint64 {
	if n >= 8 {
		return load64(data, off)
	}
	var value uint64
	for i := 0; i < n; i++ {
		value |= uint64(data[off+i]) << (uint(i) * 8)
	}
	return value
}

func load64(data []byte, off int) uint64 {
	return *(*uint64)(unsafe.Pointer(&data[off]))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
