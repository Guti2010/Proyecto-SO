package handlers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

/* ---------------- helpers (sin colisión con otros tests) ---------------- */

func mustJSONIO[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("json.Unmarshal: %v\ninput=%q", err, s)
	}
	return v
}

// nombre único local para estos tests de IO
func ioUnique(prefix, ext string) string {
	return prefix + "_" + strconv.FormatInt(time.Now().UnixNano(), 10) + ext
}

func ioMustWrite(t *testing.T, name, content string) string {
	t.Helper()
	_ = os.MkdirAll(dataDir, 0o755)
	fp := filepath.Join(dataDir, name)
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", fp, err)
	}
	return fp
}

func ioReadInts(t *testing.T, path string) []int64 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open sorted: %v", err)
	}
	defer f.Close()
	var out []int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			continue
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("parse sorted: %v (%q)", err, s)
		}
		out = append(out, n)
	}
	return out
}

func ioIsSortedAsc(a []int64) bool {
	return sort.SliceIsSorted(a, func(i, j int) bool { return a[i] < a[j] })
}

/* ---------------- canceled / ctxErrResult ---------------- */

func TestCanceled_Variants(t *testing.T) {
	if canceled(nil) {
		t.Fatalf("nil ctx must be false")
	}
	if canceled(context.Background()) {
		t.Fatalf("background must be false")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !canceled(ctx) {
		t.Fatalf("canceled ctx must be true")
	}
}

func TestCtxErrResult_NilCtx(t *testing.T) {
	r := ctxErrResult(nil)
	if r.Status != 503 || !r.JSON || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("nil ctx => %+v", r)
	}
}

func TestCtxErrResult_NoErrCtx(t *testing.T) {
	ctx := context.Background()
	r := ctxErrResult(ctx)
	if r.Status != 503 || !r.JSON || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("no-err ctx => %+v", r)
	}
}

func TestCtxErrResult_CanceledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := ctxErrResult(ctx)
	if r.Status != 503 || !r.JSON || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("canceled ctx => %+v", r)
	}
}

/* ---------------- cleanIntLine ---------------- */

func TestCleanIntLine_BOMBytes_And_Rune(t *testing.T) {
	// BOM como bytes EF BB BF + espacios
	in := []byte{0xEF, 0xBB, 0xBF, ' ', '\t', '1', '2', '3', ' '}
	s := cleanIntLine(in)
	if s != "123" {
		t.Fatalf("cleanIntLine bytes: %q", s)
	}
	// BOM como rune U+FEFF al inicio
	in2 := []byte("\uFEFF  -42 ")
	s2 := cleanIntLine(in2)
	if strings.TrimSpace(s2) != "-42" {
		t.Fatalf("cleanIntLine rune: %q", s2)
	}
}

/* ---------------- WordCount ---------------- */

func TestWordCountJSON_Basic(t *testing.T) {
	name := ioUnique("wc", ".txt")
	lines := []string{
		"hola mundo",     // 2 palabras
		"",               // 0
		"\tesp  a  cios", // 3 (esp,a,cios)
	}
	content := strings.Join(lines, "\n") + "\n"
	ioMustWrite(t, name, content)

	r := WordCountJSON(map[string]string{"name": name})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("WordCount: %+v", r)
	}
	type out struct {
		File  string `json:"file"`
		Lines int64  `json:"lines"`
		Words int64  `json:"words"`
		Bytes int64  `json:"bytes"`
	}
	o := mustJSONIO[out](t, r.Body)
	if o.File != name {
		t.Fatalf("file mismatch: %q", o.File)
	}
	// calcula esperado
	var wantL, wantW, wantB int64
	for _, ln := range lines {
		wantL++
		wantB += int64(len(ln) + 1) // + '\n'
		inWord := false
		for i := 0; i < len(ln); i++ {
			c := ln[i]
			if c > ' ' {
				if !inWord {
					wantW++
					inWord = true
				}
			} else {
				inWord = false
			}
		}
	}
	if o.Lines != wantL || o.Words != wantW || o.Bytes != wantB {
		t.Fatalf("wc mismatch: got L/W/B=%d/%d/%d want %d/%d/%d",
			o.Lines, o.Words, o.Bytes, wantL, wantW, wantB)
	}
}

func TestWordCountJSON_Validation_And_NotFound(t *testing.T) {
	if r := WordCountJSON(map[string]string{}); r.Status != 400 {
		t.Fatalf("missing name -> 400: %+v", r)
	}
	if r := WordCountJSON(map[string]string{"name": "../evil"}); r.Status != 400 {
		t.Fatalf("bad_name -> 400: %+v", r)
	}
	if r := WordCountJSON(map[string]string{"name": "nope.txt"}); r.Status != 404 {
		t.Fatalf("not found -> 404: %+v", r)
	}
}

/* ---------------- Grep ---------------- */

func TestGrepJSON_Basic(t *testing.T) {
	name := ioUnique("grep", ".txt")
	content := "uno\ndos\ntres\ndos\n"
	ioMustWrite(t, name, content)

	r := GrepJSON(map[string]string{"name": name, "pattern": "dos"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("grep: %+v", r)
	}
	type out struct {
		File    string   `json:"file"`
		Pattern string   `json:"pattern"`
		Matches int      `json:"matches"`
		First   []string `json:"first"`
	}
	o := mustJSONIO[out](t, r.Body)
	if o.File != name || o.Pattern != "dos" || o.Matches != 2 {
		t.Fatalf("grep payload: %+v", o)
	}
	if len(o.First) != 2 || o.First[0] != "dos" || o.First[1] != "dos" {
		t.Fatalf("grep first: %+v", o.First)
	}
}

func TestGrepJSON_Validation(t *testing.T) {
	if r := GrepJSON(map[string]string{}); r.Status != 400 {
		t.Fatalf("missing -> 400: %+v", r)
	}
	if r := GrepJSON(map[string]string{"name": "../x", "pattern": "a"}); r.Status != 400 {
		t.Fatalf("bad_name -> 400: %+v", r)
	}
	if r := GrepJSON(map[string]string{"name": "nope.txt", "pattern": "a"}); r.Status != 404 {
		t.Fatalf("not found -> 404: %+v", r)
	}
	if r := GrepJSON(map[string]string{"name": "x", "pattern": "("}); r.Status != 400 {
		t.Fatalf("bad regex -> 400: %+v", r)
	}
}

/* ---------------- HashFile ---------------- */

func TestHashFileJSON_OK_And_Cancel(t *testing.T) {
	name := ioUnique("hash", ".txt")
	content := "abc\nxyz\n"
	fp := ioMustWrite(t, name, content)

	// OK
	r := HashFileJSON(map[string]string{"name": name})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("hash ok: %+v", r)
	}
	type out struct {
		File string `json:"file"`
		Algo string `json:"algo"`
		Hex  string `json:"hex"`
	}
	o := mustJSONIO[out](t, r.Body)
	sum := sha256.Sum256([]byte(content))
	if o.File != name || o.Algo != "sha256" || o.Hex != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash payload: %+v exp hex=%s", o, hex.EncodeToString(sum[:]))
	}

	// Cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rc := HashFileJSONCtx(ctx, map[string]string{"name": name})
	if rc.Status != 503 || rc.Err == nil {
		t.Fatalf("hash canceled -> 503: %+v", rc)
	}

	_ = os.Remove(fp)
}

func TestHashFileJSON_Validation(t *testing.T) {
	if r := HashFileJSON(map[string]string{}); r.Status != 400 {
		t.Fatalf("missing -> 400: %+v", r)
	}
	if r := HashFileJSON(map[string]string{"name": "../x"}); r.Status != 400 {
		t.Fatalf("bad_name -> 400: %+v", r)
	}
	if r := HashFileJSON(map[string]string{"name": "x", "algo": "md5"}); r.Status != 400 {
		t.Fatalf("bad algo -> 400: %+v", r)
	}
	if r := HashFileJSON(map[string]string{"name": "nope.txt"}); r.Status != 404 {
		t.Fatalf("not found -> 404: %+v", r)
	}
}

/* ---------------- SortFile (quick + merge) ---------------- */

func TestSortFileJSON_Quick_InMemory(t *testing.T) {
	name := ioUnique("sortq", ".txt")
	// incluye líneas vacías y BOM en una línea
	content := "10\n3\n\n" + string([]byte{0xEF, 0xBB, 0xBF}) + "7\n-1\n5\n"
	in := ioMustWrite(t, name, content)

	r := SortFileJSON(map[string]string{"name": name, "algo": "quick"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("sort quick: %+v", r)
	}
	var out struct {
		SortedFile string `json:"sorted_file"`
		Algo       string `json:"algo"`
		Chunks     int    `json:"chunks"`
	}
	out = mustJSONIO[struct {
		SortedFile string `json:"sorted_file"`
		Algo       string `json:"algo"`
		Chunks     int    `json:"chunks"`
	}](t, r.Body)

	if out.Algo != "quick" || out.Chunks != 1 {
		t.Fatalf("payload: %+v", out)
	}

	sortedPath := filepath.Join(dataDir, out.SortedFile)
	ints := ioReadInts(t, sortedPath)
	if !ioIsSortedAsc(ints) {
		t.Fatalf("not sorted asc: %v", ints)
	}

	_ = os.Remove(in)
	_ = os.Remove(sortedPath)
}

func TestSortFileJSON_Merge_WithChunks_And_Cancel(t *testing.T) {
	name := ioUnique("sortm", ".txt")
	// 9 números, chunksize=3 → 3 chunks
	nums := []int{9, 1, 5, 2, 8, 3, 7, 4, 6}
	var sb strings.Builder
	for _, n := range nums {
		sb.WriteString(strconv.Itoa(n))
		sb.WriteByte('\n')
	}
	in := ioMustWrite(t, name, sb.String())

	// OK (merge)
	r := SortFileJSON(map[string]string{"name": name, "algo": "merge", "chunksize": "3"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("sort merge: %+v", r)
	}
	var out struct {
		SortedFile string `json:"sorted_file"`
		Algo       string `json:"algo"`
		Chunks     int    `json:"chunks"`
	}
	out = mustJSONIO[struct {
		SortedFile string `json:"sorted_file"`
		Algo       string `json:"algo"`
		Chunks     int    `json:"chunks"`
	}](t, r.Body)

	if out.Algo != "merge" || out.Chunks != 3 {
		t.Fatalf("payload: %+v", out)
	}
	sortedPath := filepath.Join(dataDir, out.SortedFile)
	ints := ioReadInts(t, sortedPath)
	if !ioIsSortedAsc(ints) {
		t.Fatalf("not sorted asc: %v", ints)
	}

	// Cancelación: chunkLines=1 para que writeChunk detecte cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rc := SortFileJSONCtx(ctx, map[string]string{"name": name, "algo": "merge", "chunksize": "1"})
	if rc.Status != 503 || rc.Err == nil {
		t.Fatalf("merge canceled -> 503: %+v", rc)
	}

	_ = os.Remove(in)
	_ = os.Remove(sortedPath)
}

func TestExternalSort_NoChunks_Error(t *testing.T) {
	// Solo líneas vacías => no se generan chunks -> kWayMergeCtx retorna error
	name := ioUnique("empty_only", ".txt")
	_ = ioMustWrite(t, name, "\n\n   \n")
	r := SortFileJSONCtx(context.Background(), map[string]string{
		"name": name, "algo": "merge", "chunksize": "3",
	})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "sort_error" {
		t.Fatalf("no-chunks sort should be 500 sort_error: %+v", r)
	}
}

/* ---------------- kWayMergeCtx directo ---------------- */

func TestKWayMergeCtx_OK(t *testing.T) {
	c1 := ioMustWrite(t, ioUnique("chunk1", ".tmp"), "1\n4\n7\n")
	c2 := ioMustWrite(t, ioUnique("chunk2", ".tmp"), "2\n3\n5\n6\n\n")
	defer os.Remove(c1)
	defer os.Remove(c2)

	out := filepath.Join(dataDir, ioUnique("merged", ".txt"))
	defer os.Remove(out)

	if err := kWayMergeCtx(context.Background(), []string{c1, c2}, out); err != nil {
		t.Fatalf("merge: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	if strings.TrimSpace(string(b)) != "1\n2\n3\n4\n5\n6\n7" {
		t.Fatalf("merged wrong:\n%s", string(b))
	}
}

func TestKWayMergeCtx_CanceledEarly(t *testing.T) {
	c1 := ioMustWrite(t, ioUnique("chunk1", ".tmp"), "1\n4\n7\n")
	c2 := ioMustWrite(t, ioUnique("chunk2", ".tmp"), "2\n3\n5\n6\n")
	defer os.Remove(c1)
	defer os.Remove(c2)
	out := filepath.Join(dataDir, ioUnique("merged_cancel", ".txt"))
	defer os.Remove(out)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := kWayMergeCtx(ctx, []string{c1, c2}, out); err == nil {
		t.Fatalf("expected cancel error")
	}
}

/* ---------------- Compress (gzip y xz cancel) ---------------- */

func TestCompressJSON_Gzip_OK_And_Cancel_And_Validation(t *testing.T) {
	name := ioUnique("comp", ".txt")
	path := ioMustWrite(t, name, strings.Repeat("A", 128))
	defer os.Remove(path)

	// OK gzip
	r := CompressJSON(map[string]string{"name": name, "codec": "gzip"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("compress gzip: %+v", r)
	}
	type out struct {
		File     string `json:"file"`
		Codec    string `json:"codec"`
		Output   string `json:"output"`
		BytesIn  int64  `json:"bytes_in"`
		BytesOut int64  `json:"bytes_out"`
	}
	o := mustJSONIO[out](t, r.Body)
	if o.File != name || o.Codec != "gzip" || !strings.HasSuffix(o.Output, ".gz") {
		t.Fatalf("payload mismatch: %+v", o)
	}
	gzPath := filepath.Join(dataDir, o.Output)
	defer os.Remove(gzPath)

	// Cancel gzip
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rc := CompressJSONCtx(ctx, map[string]string{"name": name, "codec": "gzip"})
	if rc.Status != 503 || rc.Err == nil {
		t.Fatalf("compress canceled -> 503: %+v", rc)
	}

	// Validaciones
	if r := CompressJSON(map[string]string{}); r.Status != 400 {
		t.Fatalf("missing name -> 400: %+v", r)
	}
	if r := CompressJSON(map[string]string{"name": "../x"}); r.Status != 400 {
		t.Fatalf("bad name -> 400: %+v", r)
	}
	if r := CompressJSON(map[string]string{"name": "nope.txt"}); r.Status != 404 {
		t.Fatalf("not found -> 404: %+v", r)
	}
	if r := CompressJSON(map[string]string{"name": name, "codec": "rar"}); r.Status != 400 {
		t.Fatalf("bad codec -> 400: %+v", r)
	}
}

func TestCompressJSONCtx_XZ_Cancel(t *testing.T) {
	name := ioUnique("comp_xz", ".txt")
	_ = ioMustWrite(t, name, strings.Repeat("A", 1024))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := CompressJSONCtx(ctx, map[string]string{"name": name, "codec": "xz"})
	if r.Status != 503 || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("xz canceled -> 503: %+v", r)
	}
}

func TestCleanIntLine_AllBranches_New(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "NoBOM_trimSpaces",
			in:   []byte("   123  \n"),
			want: "123",
		},
		{
			name: "BOMBytesAtStart_removed",
			in:   []byte{0xEF, 0xBB, 0xBF, '1', '2', '\n'},
			want: "12",
		},
		{
			// espacios al principio → el primer if NO quita BOM bytes.
			// Tras TrimSpace queda BOM como rune al inicio y se elimina en la 2ª rama.
			name: "SpacesThenBOM_asRune_removed",
			in:   []byte{' ', '\t', 0xEF, 0xBB, 0xBF, '4', '2', ' ', '\n'},
			want: "42",
		},
		{
			// BOM en medio NO debe tocarse (solo se elimina si es prefijo después de trim).
			name: "BOMInMiddle_kept",
			in:   []byte("12\uFEFF34  \n"),
			want: "12\uFEFF34",
		},
		{
			name: "OnlyWhitespace_empty",
			in:   []byte(" \t \n"),
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanIntLine(tc.in)
			if got != tc.want {
				t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

// -------- sortInMemoryCtx: ramas de error que faltaban --------

func TestSortInMemoryCtx_ParseError_New(t *testing.T) {
    // Contiene "x" que no es entero => strconv.ParseInt falla.
    in := ioMustWrite(t, ioUnique("badint", ".txt"), "1\nx\n3\n")
    out := filepath.Join(dataDir, ioUnique("out_badint", ".txt"))

    _, err := sortInMemoryCtx(context.Background(), in, out)
    if err == nil || !strings.Contains(err.Error(), "parse int") {
        t.Fatalf("esperaba error de parseo, got: %v", err)
    }
    _ = os.Remove(out)
}

func TestSortInMemoryCtx_ScannerErr_TokenTooLong_New(t *testing.T) {
    // Fuerza error del Scanner: línea > max token size (1<<20).
    hugeLine := strings.Repeat("9", (1<<20)+10) + "\n"
    in := ioMustWrite(t, ioUnique("toolong", ".txt"), hugeLine)
    out := filepath.Join(dataDir, ioUnique("out_toolong", ".txt"))

    _, err := sortInMemoryCtx(context.Background(), in, out)
    if err == nil {
        t.Fatalf("esperaba error del scanner (token demasiado largo)")
    }
    _ = os.Remove(out)
}

func TestSortFileJSONCtx_DefaultsToMerge_WhenAlgoInvalid(t *testing.T) {
	name := ioUnique("sort_default_merge", ".txt")
	_ = ioMustWrite(t, name, "5\n1\n4\n2\n3\n")

	// Pongo algo inválido en algo -> debe fallback a "merge"
	r := SortFileJSONCtx(context.Background(), map[string]string{
		"name": name,
		"algo": "whatever",
		// chunksize pequeño para que realmente pase por externalSort
		"chunksize": "2",
	})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("SortFileJSONCtx fallback: %+v", r)
	}
	var out struct {
		Algo       string `json:"algo"`
		SortedFile string `json:"sorted_file"`
	}
	if err := json.Unmarshal([]byte(r.Body), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if out.Algo != "merge" {
		t.Fatalf("algo fallback: want 'merge', got %q", out.Algo)
	}
	sorted := filepath.Join(dataDir, out.SortedFile)
	defer os.Remove(sorted)

	ints := ioReadInts(t, sorted)
	if !ioIsSortedAsc(ints) {
		t.Fatalf("not sorted asc: %v", ints)
	}
}

func TestSortInMemoryCtx_CreateOutPathError(t *testing.T) {
	in := ioUnique("in_mem_err", ".txt")
	_ = ioMustWrite(t, in, "3\n2\n1\n")

	// Creo un directorio y lo paso como outPath -> os.Create(outPath) debe fallar.
	outDir := filepath.Join(dataDir, ioUnique("dir_as_out", ""))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := sortInMemoryCtx(context.Background(),
		filepath.Join(dataDir, in), // inPath completo
		outDir,                      // outPath apunta a un directorio
	)
	if err == nil {
		t.Fatalf("esperaba error al crear archivo de salida (out es directorio)")
	}
	_ = os.RemoveAll(outDir)
}

func TestExternalSortCtx_SingleChunk_RenamesTemp(t *testing.T) {
	// Pocos números y chunkLines grande -> se genera un único chunk y se renombra.
	name := ioUnique("single_chunk", ".txt")
	_ = ioMustWrite(t, name, "10\n7\n9\n")

	inPath := filepath.Join(dataDir, name)
	outPath := filepath.Join(dataDir, ioUnique("single_chunk_out", ".sorted"))

	chunks, err := externalSortCtx(context.Background(), inPath, outPath, 1_000_000)
	if err != nil {
		t.Fatalf("externalSortCtx: %v", err)
	}
	if chunks != 1 {
		t.Fatalf("want chunks=1, got %d", chunks)
	}
	// Debe existir el archivo de salida final (tras el Rename)
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("outPath not found after rename: %v", err)
	}
	ints := ioReadInts(t, outPath)
	if !ioIsSortedAsc(ints) {
		t.Fatalf("not sorted asc: %v", ints)
	}
	_ = os.Remove(outPath)
}

// ========================= SortFileJSONCtx: más ramas =========================

func TestSortFileJSONCtx_Quick_ScannerErr_MapsTo500(t *testing.T) {
	name := ioUnique("quick_scanner_err", ".txt")
	// línea > 1<<20 para disparar error del Scanner en sortInMemoryCtx
	huge := strings.Repeat("9", (1<<20)+32) + "\n"
	_ = ioMustWrite(t, name, huge)

	r := SortFileJSONCtx(context.Background(), map[string]string{
		"name": name, "algo": "quick",
	})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "sort_error" {
		t.Fatalf("quick scanner err -> 500 sort_error: %+v", r)
	}
}

func TestSortFileJSONCtx_BadName_And_NotFound(t *testing.T) {
	if r := SortFileJSONCtx(context.Background(), map[string]string{
		"name": "../bad", "algo": "merge",
	}); r.Status != 400 {
		t.Fatalf("bad name -> 400: %+v", r)
	}
	if r := SortFileJSONCtx(context.Background(), map[string]string{
		"name": "does-not-exist.txt", "algo": "merge",
	}); r.Status != 404 {
		t.Fatalf("not found -> 404: %+v", r)
	}
}

func TestSortFileJSONCtx_ReportsBytesInOut(t *testing.T) {
	name := ioUnique("bytes_in_out", ".txt")
	content := "3\n1\n2\n"
	path := ioMustWrite(t, name, content)
	defer os.Remove(path)

	r := SortFileJSONCtx(context.Background(), map[string]string{
		"name": name, "algo": "merge", "chunksize": "2",
	})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("merge ok: %+v", r)
	}
	var out struct {
		BytesIn   int64  `json:"bytes_in"`
		BytesOut  int64  `json:"bytes_out"`
		SortedFile string `json:"sorted_file"`
	}
	if err := json.Unmarshal([]byte(r.Body), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	stat, _ := os.Stat(filepath.Join(dataDir, name))
	if out.BytesIn != stat.Size() {
		t.Fatalf("bytes_in mismatch: got %d want %d", out.BytesIn, stat.Size())
	}
	if out.BytesOut <= 0 {
		t.Fatalf("bytes_out should be >0, got %d", out.BytesOut)
	}
	_ = os.Remove(filepath.Join(dataDir, out.SortedFile))
}

// ========================= sortInMemoryCtx: más ramas =========================

func TestSortInMemoryCtx_CanceledEarly(t *testing.T) {
	in := ioUnique("in_mem_cancel", ".txt")
	_ = ioMustWrite(t, in, "3\n2\n1\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelado antes de empezar -> se corta en el primer check
	_, err := sortInMemoryCtx(ctx, filepath.Join(dataDir, in),
		filepath.Join(dataDir, ioUnique("out_cancel", ".sorted")))
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("esperaba context.Canceled, got %v", err)
	}
}

func TestSortInMemoryCtx_OK_Direct(t *testing.T) {
	in := ioUnique("in_mem_ok", ".txt")
	_ = ioMustWrite(t, in, "10\n5\n7\n")
	out := filepath.Join(dataDir, ioUnique("in_mem_ok_out", ".sorted"))
	defer os.Remove(out)

	chunks, err := sortInMemoryCtx(context.Background(),
		filepath.Join(dataDir, in), out)
	if err != nil || chunks != 1 {
		t.Fatalf("sortInMemoryCtx ok: chunks=%d err=%v", chunks, err)
	}
	ints := ioReadInts(t, out)
	if !ioIsSortedAsc(ints) {
		t.Fatalf("not sorted asc: %v", ints)
	}
}

// ========================= externalSortCtx: más ramas =========================

func TestExternalSortCtx_ParseError(t *testing.T) {
	in := ioUnique("ext_parse_err", ".txt")
	_ = ioMustWrite(t, in, "1\nx\n2\n")
	out := filepath.Join(dataDir, ioUnique("ext_parse_err_out", ".sorted"))
	_, err := externalSortCtx(context.Background(),
		filepath.Join(dataDir, in), out, 2)
	if err == nil || !strings.Contains(err.Error(), "parse int") {
		t.Fatalf("esperaba error de parseo, got %v", err)
	}
}

func TestExternalSortCtx_MultiChunks_OK_Direct(t *testing.T) {
	in := ioUnique("ext_multi_ok", ".txt")
	_ = ioMustWrite(t, in, "9\n1\n5\n2\n8\n3\n7\n4\n6\n")
	out := filepath.Join(dataDir, ioUnique("ext_multi_ok_out", ".sorted"))
	defer os.Remove(out)

	chunks, err := externalSortCtx(context.Background(),
		filepath.Join(dataDir, in), out, 3) // 9 números -> 3 chunks
	if err != nil || chunks != 3 {
		t.Fatalf("externalSortCtx ok: chunks=%d err=%v", chunks, err)
	}
	ints := ioReadInts(t, out)
	if !ioIsSortedAsc(ints) {
		t.Fatalf("not sorted asc: %v", ints)
	}
}

func TestExternalSortCtx_CancelInsideWriteChunk(t *testing.T) {
	in := ioUnique("ext_cancel_write", ".txt")
	// Suficientes números para entrar al loop de writeChunk
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString(strconv.Itoa(5000 - i))
		sb.WriteByte('\n')
	}
	_ = ioMustWrite(t, in, sb.String())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelado antes — writeChunk detecta cancelación
	_, err := externalSortCtx(ctx,
		filepath.Join(dataDir, in),
		filepath.Join(dataDir, ioUnique("ext_cancel_write_out", ".sorted")),
		1000, // forzamos varios writeChunk
	)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("esperaba context.Canceled desde writeChunk, got %v", err)
	}
}


// ---------- kWayMergeCtx: más ramas de error y casos esquina ----------

func TestKWayMergeCtx_OpenError(t *testing.T) {
	out := filepath.Join(dataDir, ioUnique("out_open_err", ".txt"))
	if err := kWayMergeCtx(context.Background(),
		[]string{filepath.Join(dataDir, "__no_such_chunk__.tmp")}, out); err == nil {
		t.Fatalf("expected open error")
	}
}

func TestKWayMergeCtx_InitScannerErr_TooLong(t *testing.T) {
	p := ioMustWrite(t, ioUnique("scan_err", ".tmp"), strings.Repeat("9", (1<<20)+32)+"\n")
	defer os.Remove(p)
	out := filepath.Join(dataDir, ioUnique("out_scan_err", ".txt"))
	if err := kWayMergeCtx(context.Background(), []string{p}, out); err == nil {
		t.Fatalf("expected scanner too-long error")
	}
}

func TestKWayMergeCtx_InitParseError(t *testing.T) {
	p := ioMustWrite(t, ioUnique("parse_err", ".tmp"), "x\n")
	defer os.Remove(p)
	out := filepath.Join(dataDir, ioUnique("out_parse_err", ".txt"))
	if err := kWayMergeCtx(context.Background(), []string{p}, out); err == nil {
		t.Fatalf("expected parse-int error")
	}
}

func TestKWayMergeCtx_BlankOnlyChunkIgnored(t *testing.T) {
	blank := ioMustWrite(t, ioUnique("blank", ".tmp"), "\n \n\t\n")
	vals  := ioMustWrite(t, ioUnique("vals", ".tmp"), "1\n2\n")
	defer os.Remove(blank)
	defer os.Remove(vals)

	out := filepath.Join(dataDir, ioUnique("out_blank_ignored", ".txt"))
	defer os.Remove(out)

	if err := kWayMergeCtx(context.Background(), []string{blank, vals}, out); err != nil {
		t.Fatalf("merge with blank chunk: %v", err)
	}
	got, _ := os.ReadFile(out)
	if strings.TrimSpace(string(got)) != "1\n2" {
		t.Fatalf("want '1\\n2', got:\n%s", string(got))
	}
}

func TestKWayMergeCtx_RuntimeScannerErr_OnNextScan(t *testing.T) {
	ok := ioMustWrite(t, ioUnique("ok", ".tmp"), "1\n2\n")
	// primera línea válida, segunda demasiado larga -> error al avanzar ese reader
	bad := ioMustWrite(t, ioUnique("bad_next", ".tmp"), "0\n"+strings.Repeat("8", (1<<20)+64)+"\n")
	defer os.Remove(ok)
	defer os.Remove(bad)

	out := filepath.Join(dataDir, ioUnique("out_runtime_scan_err", ".txt"))
	if err := kWayMergeCtx(context.Background(), []string{ok, bad}, out); err == nil {
		t.Fatalf("expected runtime scanner error on next scan")
	}
}

// ---------- CompressJSONCtx: más validaciones/errores específicos ----------

func TestCompressJSONCtx_BadCodec_And_NotFound(t *testing.T) {
	// codec inválido se valida antes de stat
	if r := CompressJSONCtx(context.Background(), map[string]string{"name": "whatever.txt", "codec": "rar"}); r.Status != 400 {
		t.Fatalf("bad codec -> 400, got: %+v", r)
	}
	// archivo inexistente
	if r := CompressJSONCtx(context.Background(), map[string]string{"name": "nope.txt", "codec": "gzip"}); r.Status != 404 {
		t.Fatalf("not found -> 404, got: %+v", r)
	}
}

func TestCompressJSONCtx_Gzip_CreateFails(t *testing.T) {
	name := ioUnique("gz_create_err", ".txt")
	_ = ioMustWrite(t, name, "A\nB\n")

	// Crea un directorio con el nombre del archivo de salida para forzar fallo en os.Create
	outDir := filepath.Join(dataDir, name+".gz")
	_ = os.MkdirAll(outDir, 0o755)
	defer os.RemoveAll(outDir)

	r := CompressJSONCtx(context.Background(), map[string]string{"name": name, "codec": "gzip"})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "fs_error" {
		t.Fatalf("gzip create failed -> 500 fs_error, got: %+v", r)
	}
}

func TestCompressJSONCtx_XZ_RealError_NoCancel(t *testing.T) {
	// Usar un directorio como "archivo" hace que `xz` falle con error propio (independiente de si está instalado)
	dirName := strings.TrimSuffix(ioUnique("dir_as_input", ".txt"), ".txt")
	dirPath := filepath.Join(dataDir, dirName)
	_ = os.MkdirAll(dirPath, 0o755)
	defer os.RemoveAll(dirPath)

	r := CompressJSONCtx(context.Background(), map[string]string{"name": dirName, "codec": "xz"})
	// Debe caer en compress_error (sea por no encontrar xz o por error al comprimir un directorio)
	if r.Status != 500 || r.Err == nil || r.Err.Code != "compress_error" {
		t.Fatalf("xz non-cancel error -> 500 compress_error, got: %+v", r)
	}
}

// ---------- kWayMergeCtx: más cobertura ----------

func TestKWayMergeCtx_CreateOutFileError(t *testing.T) {
	// Un directorio con el mismo path de salida fuerza fallo en os.Create(outPath).
	ch1 := ioMustWrite(t, ioUnique("mk_out_err1", ".tmp"), "1\n3\n")
	ch2 := ioMustWrite(t, ioUnique("mk_out_err2", ".tmp"), "2\n4\n")
	defer os.Remove(ch1)
	defer os.Remove(ch2)

	out := filepath.Join(dataDir, ioUnique("out_is_dir", "")) // sin extensión
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(out)

	if err := kWayMergeCtx(context.Background(), []string{ch1, ch2}, out); err == nil {
		t.Fatalf("expected create(out) error when outPath is a directory")
	}
}

func TestKWayMergeCtx_AllBlankChunks_ProducesEmptyFile(t *testing.T) {
	c1 := ioMustWrite(t, ioUnique("blank1", ".tmp"), "\n \n\t\n")
	c2 := ioMustWrite(t, ioUnique("blank2", ".tmp"), "\n\n")
	defer os.Remove(c1)
	defer os.Remove(c2)

	out := filepath.Join(dataDir, ioUnique("out_all_blank", ".txt"))
	defer os.Remove(out)

	if err := kWayMergeCtx(context.Background(), []string{c1, c2}, out); err != nil {
		t.Fatalf("merge with all-blank chunks: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat out: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected empty output, size=%d", info.Size())
	}
}

func TestKWayMergeCtx_OpenError_SecondChunk(t *testing.T) {
	ok := ioMustWrite(t, ioUnique("ok_chunk", ".tmp"), "1\n2\n")
	defer os.Remove(ok)

	missing := filepath.Join(dataDir, "__missing_chunk__.tmp")
	out := filepath.Join(dataDir, ioUnique("out_open2", ".txt"))
	if err := kWayMergeCtx(context.Background(), []string{ok, missing}, out); err == nil {
		t.Fatalf("expected open error on second chunk")
	}
}

func TestKWayMergeCtx_NextParseError(t *testing.T) {
	// Primeros valores serán encolados; al avanzar uno de los readers aparece "x" -> parse falla.
	a := ioMustWrite(t, ioUnique("next_parse_a", ".tmp"), "0\n1\nx\n2\n")
	b := ioMustWrite(t, ioUnique("next_parse_b", ".tmp"), "3\n4\n5\n")
	defer os.Remove(a)
	defer os.Remove(b)

	out := filepath.Join(dataDir, ioUnique("out_next_parse", ".txt"))
	if err := kWayMergeCtx(context.Background(), []string{a, b}, out); err == nil {
		t.Fatalf("expected parse error on next scan")
	}
	_ = os.Remove(out)
}

// ---------- CompressJSONCtx: más cobertura ----------

func TestCompressJSONCtx_DefaultGzip_NoCodecParam(t *testing.T) {
	name := ioUnique("def_gz", ".txt")
	path := ioMustWrite(t, name, strings.Repeat("B", 256))
	defer os.Remove(path)

	r := CompressJSONCtx(context.Background(), map[string]string{"name": name}) // sin "codec"
	if r.Status != 200 || !r.JSON {
		t.Fatalf("default gzip: %+v", r)
	}
	var out struct {
		File     string `json:"file"`
		Codec    string `json:"codec"`
		Output   string `json:"output"`
		BytesIn  int64  `json:"bytes_in"`
		BytesOut int64  `json:"bytes_out"`
	}
	if err := json.Unmarshal([]byte(r.Body), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if out.File != name || out.Codec != "gzip" || !strings.HasSuffix(out.Output, ".gz") || out.BytesIn <= 0 {
		t.Fatalf("payload mismatch: %+v", out)
	}
	_ = os.Remove(filepath.Join(dataDir, out.Output))
}

func TestCompressJSONCtx_Gzip_CancelMidStream(t *testing.T) {
	// Archivo un poco más grande para que haya varias iteraciones de lectura/escritura.
	name := ioUnique("cancel_mid", ".txt")
	var sb strings.Builder
	for i := 0; i < 4<<20; i += 64 { // ~4MiB en bloques
		sb.WriteString("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ\n")
	}
	_ = ioMustWrite(t, name, sb.String())

	ctx, cancel := context.WithCancel(context.Background())
	// Cancela "en mitad" del loop (pequeño retardo).
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	r := CompressJSONCtx(ctx, map[string]string{"name": name, "codec": "gzip"})
	if r.Status != 503 || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("mid-stream cancel -> 503 canceled, got: %+v", r)
	}
}

// ---------- CompressJSONCtx: más ramas de error ----------

func TestCompressJSONCtx_Gzip_CreateOutPathFails(t *testing.T) {
	// 1) archivo de entrada válido
	name := ioUnique("gz_create_err", ".txt")
	inFile := ioMustWrite(t, name, "hello\nworld\n")
	defer os.Remove(inFile)

	// 2) crear un DIRECTORIO con el mismo nombre del archivo de salida .gz
	outDir := filepath.Join(dataDir, name) + ".gz"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir outDir: %v", err)
	}
	defer os.RemoveAll(outDir)

	// 3) la creación del .gz debe fallar (fs_error: create failed)
	r := CompressJSONCtx(context.Background(), map[string]string{"name": name, "codec": "gzip"})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "fs_error" {
		t.Fatalf("expected fs_error(create failed): %+v", r)
	}
}

func TestCompressJSONCtx_Gzip_ReadErrorWhenInputIsDirectory(t *testing.T) {
	// El "archivo" de entrada es un directorio; stat OK, pero leer fallará.
	name := ioUnique("gz_read_dir", "") // sin .txt a propósito
	inDir := filepath.Join(dataDir, name)
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		t.Fatalf("mkdir inDir: %v", err)
	}
	defer os.RemoveAll(inDir)

	// No crees el .gz para que os.Create() sí pueda crear el archivo
	r := CompressJSONCtx(context.Background(), map[string]string{"name": name, "codec": "gzip"})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "fs_error" {
		t.Fatalf("expected fs_error(read error on dir): %+v", r)
	}
	// Si llegó a crear el .gz, bórralo.
	_ = os.Remove(inDir + ".gz")
}

func TestCompressJSONCtx_XZ_ErrorOnDirectory(t *testing.T) {
	// Con "xz", usar un directorio como entrada hace fallar cmd.Run (no cancelado).
	name := ioUnique("xz_dir", "")
	inDir := filepath.Join(dataDir, name)
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		t.Fatalf("mkdir inDir: %v", err)
	}
	defer os.RemoveAll(inDir)

	r := CompressJSONCtx(context.Background(), map[string]string{"name": name, "codec": "xz"})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "compress_error" {
		t.Fatalf("expected compress_error from xz on directory: %+v", r)
	}
}

