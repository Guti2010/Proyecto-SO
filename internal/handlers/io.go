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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"so-http10-demo/internal/resp"
)

/*
   ===============================================================
   Cancelación cooperativa
   ===============================================================
*/

const checkEvery = 4096 // sonda barata de cancelación (potencias de 2 funcionan bien)

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
	// 503 para trabajos cancelados/expirados
	if ctx == nil || ctx.Err() == nil {
		return resp.Unavail("canceled", "job canceled")
	}
	return resp.Unavail("canceled", ctx.Err().Error())
}

/*
   ===============================================================
   Helper: limpiar líneas numéricas (BOM + espacios)
   ===============================================================
*/
func cleanIntLine(b []byte) string {
	// Quitar BOM UTF-8 a nivel de bytes (EF BB BF)
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	// Recortar espacios y, si quedó el BOM como rune en el string, quitarlo
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "\uFEFF") { // BOM como rune
		s = strings.TrimPrefix(s, "\uFEFF")
		s = strings.TrimSpace(s)
	}
	return s
}


/*
   ===============================================================
   /wordcount?name=FILE
   - Cuenta líneas, palabras y bytes (tipo `wc`).
   - Soporta archivos grandes (lectura streaming).
   Respuesta (orden estable):
     {"file":..., "lines":N, "words":N, "bytes":N, "elapsed_ms":N}
   ===============================================================
*/

// Wrapper sin ctx para compatibilidad
func WordCountJSON(params map[string]string) resp.Result {
	return WordCountJSONCtx(context.Background(), params)
}

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
	// Si esperas líneas muy grandes, descomenta y ajusta:
	// sc.Buffer(make([]byte, 0, 1<<20), 1<<20)

	i := 0
	for sc.Scan() {
		if i&(checkEvery-1) == 0 && canceled(ctx) {
			return ctxErrResult(ctx)
		}
		i++

		lines++
		b := sc.Bytes()
		bytes += int64(len(b) + 1) // +1 por '\n' (Scanner quita el salto)

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

	type out struct {
		File      string `json:"file"`
		Lines     int64  `json:"lines"`
		Words     int64  `json:"words"`
		Bytes     int64  `json:"bytes"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}
	b, _ := json.Marshal(out{
		File: path, Lines: lines, Words: words, Bytes: bytes,
		ElapsedMS: time.Since(start).Milliseconds(),
	})
	return resp.JSONOK(string(b))
}

/*
   ===============================================================
   /grep?name=FILE&pattern=REGEX
   - Devuelve número de coincidencias y las primeras 10 líneas que hacen match
   Respuesta (orden estable):
     {"file":..., "pattern":..., "matches":N, "first":[...], "elapsed_ms":N}
   ===============================================================
*/

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
		if i&(checkEvery-1) == 0 && canceled(ctx) {
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

	type out struct {
		File      string   `json:"file"`
		Pattern   string   `json:"pattern"`
		Matches   int      `json:"matches"`
		First     []string `json:"first"`
		ElapsedMS int64    `json:"elapsed_ms"`
	}
	b, _ := json.Marshal(out{
		File: path, Pattern: pat, Matches: matches, First: first,
		ElapsedMS: time.Since(start).Milliseconds(),
	})
	return resp.JSONOK(string(b))
}

/*
   ===============================================================
   /hashfile?name=FILE&algo=sha256
   - Calcula hash SHA-256 streaming.
   Respuesta (orden estable):
     {"file":..., "algo":"sha256", "hex":"...", "elapsed_ms":N}
   ===============================================================
*/

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

	type out struct {
		File      string `json:"file"`
		Algo      string `json:"algo"`
		Hex       string `json:"hex"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}
	b, _ := json.Marshal(out{
		File: path, Algo: "sha256", Hex: hex.EncodeToString(h.Sum(nil)),
		ElapsedMS: time.Since(start).Milliseconds(),
	})
	return resp.JSONOK(string(b))
}

/*
   ===============================================================
   /sortfile?name=FILE&algo=merge|quick[&chunksize=N]
   - Ordena enteros (uno por línea).
   - "merge": external sort (para archivos >= 50MB).
   - "quick": in-memory (rápido si cabe en RAM).
   Respuesta (orden estable):
     {"file":..., "algo":..., "sorted_file":..., "chunks":N, "bytes_in":N,
      "bytes_out":N, "elapsed_ms":N}
   ===============================================================
*/

func SortFileJSON(params map[string]string) resp.Result {
	return SortFileJSONCtx(context.Background(), params)
}

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
		algo = "merge" // por defecto: external sort (más robusto)
	}
	chunkSize := 1_000_000 // líneas por chunk en modo merge
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
	var bytesOut int64
	if outInfo != nil {
		bytesOut = outInfo.Size()
	}

	type out struct {
		File       string `json:"file"`
		Algo       string `json:"algo"`
		SortedFile string `json:"sorted_file"`
		Chunks     int    `json:"chunks"`
		BytesIn    int64  `json:"bytes_in"`
		BytesOut   int64  `json:"bytes_out"`
		ElapsedMS  int64  `json:"elapsed_ms"`
	}
	b, _ := json.Marshal(out{
		File: base, Algo: algo, SortedFile: filepath.Base(outPath),
		Chunks: chunks, BytesIn: bytesIn, BytesOut: bytesOut,
		ElapsedMS: time.Since(start).Milliseconds(),
	})
	return resp.JSONOK(string(b))
}

// sort en memoria (rápido si cabe en RAM)
func sortInMemoryCtx(ctx context.Context, inPath, outPath string) (int, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var nums []int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)

	i := 0
	for sc.Scan() {
		if i&(checkEvery-1) == 0 && canceled(ctx) {
			return 0, context.Canceled
		}
		i++

		s := cleanIntLine(sc.Bytes())
		if s == "" {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
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
	return 1, nil // un solo "chunk" lógico
}

// external sort (divide y fusiona k-way)
func externalSortCtx(ctx context.Context, inPath, outPath string, chunkLines int) (int, error) {
	in, err := os.Open(inPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	var chunkFiles []string
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 4<<20), 4<<20)

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
		if i&(checkEvery-1) == 0 && canceled(ctx) {
			return 0, context.Canceled
		}
		i++

		s := cleanIntLine(sc.Bytes())
		if s == "" {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
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

/*
   ===============================================================
   k-way merge (min-heap)
   ===============================================================
*/

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
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		cr := &chunkReader{f: f, sc: sc}
		if cr.sc.Scan() {
			s := cleanIntLine(cr.sc.Bytes())
			for s == "" && cr.sc.Scan() {
				s = cleanIntLine(cr.sc.Bytes())
			}
			if s != "" {
				v, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					f.Close()
					return err
				}
				cr.val = v
			} else {
				cr.eof = true
			}
		} else if err := cr.sc.Err(); err != nil {
			f.Close()
			return err
		} else {
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
		if step&(checkEvery-1) == 0 && canceled(ctx) {
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
			s := cleanIntLine(r.sc.Bytes())
			for s == "" && r.sc.Scan() {
				s = cleanIntLine(r.sc.Bytes())
			}
			if s != "" {
				v, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return err
				}
				r.val = v
				heap.Push(h, minItem{val: r.val, idx: idx})
			}
		} else if err := r.sc.Err(); err != nil {
			return err
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

/*
   ===============================================================
   /compress?name=FILE&codec=gzip|xz
   - gzip: usa librería estándar.
   - xz: invoca binario del sistema `xz` (requiere xz-utils).
   Respuesta (orden estable):
     {"file":..., "codec":"gzip|xz", "output":..., "bytes_in":N,
      "bytes_out":N, "elapsed_ms":N}
   ===============================================================
*/

func CompressJSON(params map[string]string) resp.Result {
	return CompressJSONCtx(context.Background(), params)
}

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
	if codec != "gzip" && codec != "xz" {
		return resp.BadReq("codec", "codec must be gzip|xz")
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

	start := time.Now()

	// Estructura común para salida (mantiene orden estable de campos)
	type compressOut struct {
		File      string `json:"file"`
		Codec     string `json:"codec"`
		Output    string `json:"output"`
		BytesIn   int64  `json:"bytes_in"`
		BytesOut  int64  `json:"bytes_out"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}

	switch codec {

	case "gzip":
		outPath := inPath + ".gz"

		in, err := os.Open(inPath)
		if err != nil {
			return resp.IntErr("fs_error", "open failed")
		}
		defer in.Close()

		fOut, err := os.Create(outPath) // trunca si existe
		if err != nil {
			return resp.IntErr("fs_error", "create failed")
		}
		defer fOut.Close()

		zw, err := gzip.NewWriterLevel(fOut, gzip.BestSpeed)
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

		body := compressOut{
			File:      base,
			Codec:     "gzip",
			Output:    filepath.Base(outPath),
			BytesIn:   bytesIn,
			BytesOut:  bytesOut,
			ElapsedMS: time.Since(start).Milliseconds(),
		}
		b, _ := json.Marshal(body)
		return resp.JSONOK(string(b))

	case "xz":
		// Invocamos el binario del sistema `xz`:
		//  -T0 : usa todos los hilos
		//  -k  : conserva el archivo original
		//  -f  : sobrescribe si existe
		cmd := exec.CommandContext(ctx, "xz", "-T0", "-k", "-f", inPath)
		if err := cmd.Run(); err != nil {
			if ctx != nil && ctx.Err() != nil { // cancelación/timeout
				return ctxErrResult(ctx)
			}
			return resp.IntErr("compress_error", err.Error())
		}

		outPath := inPath + ".xz"
		outInfo, _ := os.Stat(outPath)
		var bytesOut int64
		if outInfo != nil {
			bytesOut = outInfo.Size()
		}

		body := compressOut{
			File:      base,
			Codec:     "xz",
			Output:    filepath.Base(outPath),
			BytesIn:   bytesIn,
			BytesOut:  bytesOut,
			ElapsedMS: time.Since(start).Milliseconds(),
		}
		b, _ := json.Marshal(body)
		return resp.JSONOK(string(b))
	}

	// No debería ejecutarse.
	return resp.IntErr("codec", "unsupported codec")
}
