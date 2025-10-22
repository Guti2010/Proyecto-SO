package handlers

import (
	"bufio"
	"compress/gzip"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"so-http10-demo/internal/resp"
)

// Nota: se reutilizan sanitize(name) y dataDir (definidos en files.go).
// dataDir := "/app/data"

// -------- util cancelación --------

const checkEvery = 4096 // frecuencia de sondeo barata (potencia de 2 ideal)

func canceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func ctxErrResult(ctx context.Context) resp.Result {
	if ctx == nil {
		return resp.Unavail("canceled", "job canceled")
	}
	// devolvemos 503 con detalle
	return resp.Unavail("canceled", ctx.Err().Error())
}

// ---------- /wordcount ----------
// Wrappers sin ctx para compatibilidad.
func WordCountJSON(params map[string]string) resp.Result {
	return WordCountJSONCtx(context.Background(), params)
}

// Cuenta líneas, palabras y bytes con cancelación cooperativa.
func WordCountJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	name := params["name"]
	if name == "" {
		return resp.BadReq("name", "file name required")
	}
	path, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}

	fp := filepath.Join(dataDir, path)
	f, err := os.Open(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "open failed")
	}
	defer f.Close()

	start := time.Now()
	var lines, words, bytes int64

	sc := bufio.NewScanner(f)
	// Si esperas líneas enormes, ajusta (hasta varios MB)
	// sc.Buffer(make([]byte, 0, 1<<20), 1<<20)

	i := 0
	for sc.Scan() {
		if i& (checkEvery-1) == 0 && canceled(ctx) {
			return ctxErrResult(ctx)
		}
		i++

		lines++
		b := sc.Bytes()
		bytes += int64(len(b) + 1) // +1 por '\n'
		inWord := false
		for _, c := range b {
			if c > ' ' {
				if !inWord {
					words++
					inWord = true
				}
			} else {
				inWord = false
			}
		}
	}
	if err := sc.Err(); err != nil {
		return resp.IntErr("fs_error", "scan error")
	}

	out := map[string]any{
		"file":       path,
		"lines":      lines,
		"words":      words,
		"bytes":      bytes,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /grep ----------
func GrepJSON(params map[string]string) resp.Result {
	return GrepJSONCtx(context.Background(), params)
}

func GrepJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	name := params["name"]
	pat := params["pattern"]
	if name == "" || pat == "" {
		return resp.BadReq("params", "name and pattern required")
	}
	path, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return resp.BadReq("pattern", "invalid regex")
	}

	fp := filepath.Join(dataDir, path)
	f, err := os.Open(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "open failed")
	}
	defer f.Close()

	start := time.Now()
	sc := bufio.NewScanner(f)
	matches := 0
	first := make([]string, 0, 10)

	i := 0
	for sc.Scan() {
		if i& (checkEvery-1) == 0 && canceled(ctx) {
			return ctxErrResult(ctx)
		}
		i++

		line := sc.Text()
		if re.MatchString(line) {
			matches++
			if len(first) < 10 {
				first = append(first, line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return resp.IntErr("fs_error", "scan error")
	}
	out := map[string]any{
		"file":       path,
		"pattern":    pat,
		"matches":    matches,
		"first":      first,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /hashfile ----------
func HashFileJSON(params map[string]string) resp.Result {
	return HashFileJSONCtx(context.Background(), params)
}

func HashFileJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	name := params["name"]
	algo := params["algo"]
	if algo == "" {
		algo = "sha256"
	}
	if algo != "sha256" {
		return resp.BadReq("algo", "only sha256 is supported for now")
	}
	if name == "" {
		return resp.BadReq("name", "file name required")
	}
	path, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}

	fp := filepath.Join(dataDir, path)
	f, err := os.Open(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "open failed")
	}
	defer f.Close()

	start := time.Now()
	h := sha256.New()

	buf := make([]byte, 1<<20) // 1 MiB
	for {
		if canceled(ctx) {
			return ctxErrResult(ctx)
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				return resp.IntErr("fs_error", "hash write error")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return resp.IntErr("fs_error", "read error")
		}
	}

	out := map[string]any{
		"file":       path,
		"algo":       "sha256",
		"hex":        hex.EncodeToString(h.Sum(nil)),
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /sortfile ----------
func SortFileJSON(params map[string]string) resp.Result {
	return SortFileJSONCtx(context.Background(), params)
}

/*
Ordena enteros (uno por línea). Si el archivo es grande, hace external sort
(divide en chunks ordenados y fusiona k-way).
*/
func SortFileJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	name := params["name"]
	if name == "" {
		return resp.BadReq("name", "file name required")
	}
	base, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}
	inPath := filepath.Join(dataDir, base)
	outPath := inPath + ".sorted"

	algo := params["algo"]
	if algo != "quick" && algo != "merge" {
		algo = "merge"
	}
	chunkSize := 1_000_000
	if v, err := strconv.Atoi(params["chunksize"]); err == nil && v > 0 {
		chunkSize = v
	}

	info, err := os.Stat(inPath)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "stat failed")
	}
	bytesIn := info.Size()

	start := time.Now()
	var chunks int
	if algo == "quick" {
		chunks, err = sortInMemoryCtx(ctx, inPath, outPath)
	} else {
		chunks, err = externalSortCtx(ctx, inPath, outPath, chunkSize)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ctxErrResult(ctx)
		}
		return resp.IntErr("sort_error", err.Error())
	}
	outInfo, _ := os.Stat(outPath)

	out := map[string]any{
		"file":        base,
		"sorted_file": filepath.Base(outPath),
		"algo":        algo,
		"chunks":      chunks,
		"bytes_in":    bytesIn,
		"bytes_out":   func() int64 { if outInfo != nil { return outInfo.Size() }; return 0 }(),
		"elapsed_ms":  time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

func sortInMemoryCtx(ctx context.Context, inPath, outPath string) (int, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var nums []int64
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 1<<20)
	sc.Buffer(buf, 1<<20)

	i := 0
	for sc.Scan() {
		if i& (checkEvery-1) == 0 && canceled(ctx) {
			return 0, context.Canceled
		}
		i++

		if len(sc.Bytes()) == 0 {
			continue
		}
		n, err := strconv.ParseInt(string(sc.Bytes()), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse int: %w", err)
		}
		nums = append(nums, n)
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}

	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })

	out, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, 1<<20)
	for _, v := range nums {
		if canceled(ctx) {
			return 0, context.Canceled
		}
		if _, err := bw.WriteString(strconv.FormatInt(v, 10) + "\n"); err != nil {
			return 0, err
		}
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	return 1, nil
}

func externalSortCtx(ctx context.Context, inPath, outPath string, chunkLines int) (int, error) {
	in, err := os.Open(inPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	var chunkFiles []string
	sc := bufio.NewScanner(in)
	buf := make([]byte, 0, 4<<20)
	sc.Buffer(buf, 4<<20)

	nums := make([]int64, 0, chunkLines)

	writeChunk := func() (string, error) {
		if len(nums) == 0 {
			return "", nil
		}
		sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })

		tmp, err := os.CreateTemp(dataDir, "sortchunk-*")
		if err != nil {
			return "", err
		}
		bw := bufio.NewWriterSize(tmp, 1<<20)
		for _, v := range nums {
			if canceled(ctx) {
				tmp.Close()
				return "", context.Canceled
			}
			if _, err := bw.WriteString(strconv.FormatInt(v, 10) + "\n"); err != nil {
				tmp.Close()
				return "", err
			}
		}
		if err := bw.Flush(); err != nil {
			tmp.Close()
			return "", err
		}
		tmp.Close()
		name := tmp.Name()
		chunkFiles = append(chunkFiles, name)
		nums = nums[:0]
		return name, nil
	}

	i := 0
	for sc.Scan() {
		if i& (checkEvery-1) == 0 && canceled(ctx) {
			return 0, context.Canceled
		}
		i++

		if len(sc.Bytes()) == 0 {
			continue
		}
		n, err := strconv.ParseInt(string(sc.Bytes()), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse int: %w", err)
		}
		nums = append(nums, n)
		if len(nums) >= chunkLines {
			if _, err := writeChunk(); err != nil {
				return 0, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	if _, err := writeChunk(); err != nil {
		return 0, err
	}

	// si hubo un único chunk, renómbralo
	if len(chunkFiles) == 1 {
		return 1, os.Rename(chunkFiles[0], outPath)
	}

	err = kWayMergeCtx(ctx, chunkFiles, outPath)

	// limpia temporales
	for _, p := range chunkFiles {
		_ = os.Remove(p)
	}
	if err != nil {
		return len(chunkFiles), err
	}
	return len(chunkFiles), nil
}

// --- k-way merge helpers ---

type chunkReader struct {
	f   *os.File
	sc  *bufio.Scanner
	val int64
	eof bool
}

type minItem struct {
	val int64
	idx int
}

type minHeap []minItem

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].val < h[j].val }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(minItem)) }
func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func kWayMergeCtx(ctx context.Context, parts []string, outPath string) error {
	if len(parts) == 0 {
		return errors.New("no chunks")
	}
	readers := make([]*chunkReader, len(parts))
	h := &minHeap{}
	heap.Init(h)

	for i, p := range parts {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(f)
		buf := make([]byte, 0, 1<<20)
		sc.Buffer(buf, 1<<20)
		cr := &chunkReader{f: f, sc: sc}
		if cr.sc.Scan() {
			v, err := strconv.ParseInt(string(cr.sc.Bytes()), 10, 64)
			if err != nil {
				f.Close()
				return err
			}
			cr.val = v
		} else {
			if err := cr.sc.Err(); err != nil {
				f.Close()
				return err
			}
			cr.eof = true
		}
		readers[i] = cr
		if !cr.eof {
			heap.Push(h, minItem{val: cr.val, idx: i})
		}
	}

	out, err := os.Create(outPath)
	if err != nil {
		for _, r := range readers {
			_ = r.f.Close()
		}
		return err
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, 1<<20)

	step := 0
	for h.Len() > 0 {
		if step& (checkEvery-1) == 0 && canceled(ctx) {
			return context.Canceled
		}
		step++

		it := heap.Pop(h).(minItem)
		idx := it.idx
		if _, err := bw.WriteString(strconv.FormatInt(it.val, 10) + "\n"); err != nil {
			return err
		}
		// avanza ese reader
		r := readers[idx]
		if r.sc.Scan() {
			v, err := strconv.ParseInt(string(r.sc.Bytes()), 10, 64)
			if err != nil {
				return err
			}
			r.val = v
			heap.Push(h, minItem{val: r.val, idx: idx})
		} else {
			if err := r.sc.Err(); err != nil {
				return err
			}
			r.eof = true
		}
	}

	if err := bw.Flush(); err != nil {
		return err
	}
	for _, r := range readers {
		_ = r.f.Close()
	}
	return nil
}

// ---------- /compress ----------
func CompressJSON(params map[string]string) resp.Result {
	return CompressJSONCtx(context.Background(), params)
}

// Implementación solo gzip (sin dependencia externa para xz).
func CompressJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	name := params["name"]
	if name == "" {
		return resp.BadReq("name", "file name required")
	}
	base, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}

	codec := params["codec"]
	if codec == "" {
		codec = "gzip"
	}
	if codec != "gzip" {
		return resp.BadReq("codec", "only gzip is supported")
	}

	inPath := filepath.Join(dataDir, base)
	info, err := os.Stat(inPath)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "stat failed")
	}
	bytesIn := info.Size()
	outPath := inPath + ".gz"

	in, err := os.Open(inPath)
	if err != nil {
		return resp.IntErr("fs_error", "open failed")
	}
	defer in.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return resp.IntErr("fs_error", "create failed")
	}
	defer out.Close()

	start := time.Now()
	zw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		return resp.IntErr("codec", err.Error())
	}

	buf := make([]byte, 1<<20) // 1 MiB
	for {
		if canceled(ctx) {
			_ = zw.Close()
			return ctxErrResult(ctx)
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := zw.Write(buf[:n]); werr != nil {
				_ = zw.Close()
				return resp.IntErr("compress_error", werr.Error())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = zw.Close()
			return resp.IntErr("fs_error", rerr.Error())
		}
	}

	if err := zw.Close(); err != nil {
		return resp.IntErr("compress_error", err.Error())
	}

	outInfo, _ := os.Stat(outPath)
	var bytesOut int64
	if outInfo != nil {
		bytesOut = outInfo.Size()
	}

	outJSON := map[string]any{
		"file":       base,
		"codec":      "gzip",
		"output":     filepath.Base(outPath),
		"bytes_in":   bytesIn,
		"bytes_out":  bytesOut,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(outJSON)
	return resp.JSONOK(string(b))
}

// atoi utilidad local (tolerante a error).
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
