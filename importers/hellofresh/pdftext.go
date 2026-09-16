package main

// A minimal PDF text extractor for printed recipe cards. It reads only what a
// card needs: objects (including object streams), Flate-compressed content
// streams, form XObjects, simple and Type0 fonts with ToUnicode maps, and the
// text-showing operators with enough of the text state to place each run on
// the page. It deliberately scans the file for objects instead of trusting the
// cross-reference table, which card PDFs don't always get right.
//
// It is not a general PDF library: encryption, other filters, and vertical
// writing are unsupported, and anything it can't read yields no text rather
// than wrong text.

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// TextRun is one shown string, positioned in page space (points, origin at the
// bottom left).
type TextRun struct {
	Page int
	X, Y float64
	EndX float64
	Size float64
	Font string
	Text string
}

type (
	pdfName   string
	pdfString []byte
	pdfArray  []any
	pdfDict   map[string]any
	pdfRef    struct{ num, gen int }
	pdfStream struct {
		dict pdfDict
		data []byte
	}
	pdfKeyword string
)

type pdfDoc struct {
	objects map[int]any
}

var objHeaderRe = regexp.MustCompile(`(\d+)\s+(\d+)\s+obj\b`)

// ExtractPDFText returns the text runs of every page in document order.
func ExtractPDFText(data []byte) ([]TextRun, error) {
	if !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return nil, errors.New("not a PDF")
	}
	doc := &pdfDoc{objects: map[int]any{}}
	for _, m := range objHeaderRe.FindAllSubmatchIndex(data, -1) {
		num, _ := strconv.Atoi(string(data[m[2]:m[3]]))
		lx := &pdfLexer{data: data, pos: m[1]}
		v, err := lx.parseObject(true)
		if err != nil {
			continue
		}
		doc.objects[num] = v
	}
	if bytes.Contains(data, []byte("/Encrypt")) {
		if _, ok := doc.trailerLike()["Encrypt"]; ok {
			return nil, errors.New("encrypted PDFs are not supported")
		}
	}
	doc.loadObjectStreams()

	pages := doc.pages()
	if len(pages) == 0 {
		return nil, errors.New("PDF has no pages")
	}
	var runs []TextRun
	for i, pg := range pages {
		content := doc.pageContent(pg.dict)
		in := &contentInterpreter{doc: doc, page: i + 1, fonts: map[string]*pdfFont{}}
		in.run(content, pg.resources, identityMatrix, 0)
		runs = append(runs, in.runs...)
	}
	return runs, nil
}

// trailerLike finds the document's trailer dictionary, or a cross-reference
// stream dictionary standing in for it.
func (d *pdfDoc) trailerLike() pdfDict {
	for _, v := range d.objects {
		if s, ok := v.(pdfStream); ok && s.dict.name("Type") == "XRef" {
			return s.dict
		}
	}
	return pdfDict{}
}

func (d *pdfDoc) resolve(v any) any {
	for i := 0; i < 32; i++ {
		r, ok := v.(pdfRef)
		if !ok {
			return v
		}
		v = d.objects[r.num]
	}
	return nil
}

func (d *pdfDoc) dict(v any) pdfDict {
	switch t := d.resolve(v).(type) {
	case pdfDict:
		return t
	case pdfStream:
		return t.dict
	}
	return nil
}

func (d *pdfDoc) loadObjectStreams() {
	var streams []pdfStream
	for _, v := range d.objects {
		if s, ok := v.(pdfStream); ok && s.dict.name("Type") == "ObjStm" {
			streams = append(streams, s)
		}
	}
	for _, s := range streams {
		data, err := d.decodeStream(s)
		if err != nil {
			continue
		}
		n, first := int(s.dict.num("N")), int(s.dict.num("First"))
		if first <= 0 || first > len(data) {
			continue
		}
		lx := &pdfLexer{data: data[:first]}
		type entry struct{ num, off int }
		var entries []entry
		for i := 0; i < n; i++ {
			a, err1 := lx.parseObject(false)
			b, err2 := lx.parseObject(false)
			an, ok1 := a.(float64)
			bn, ok2 := b.(float64)
			if err1 != nil || err2 != nil || !ok1 || !ok2 {
				break
			}
			entries = append(entries, entry{int(an), int(bn)})
		}
		for _, e := range entries {
			if _, exists := d.objects[e.num]; exists || first+e.off >= len(data) {
				continue
			}
			olx := &pdfLexer{data: data, pos: first + e.off}
			if v, err := olx.parseObject(false); err == nil {
				d.objects[e.num] = v
			}
		}
	}
}

func (d *pdfDoc) decodeStream(s pdfStream) ([]byte, error) {
	var filters []string
	switch f := d.resolve(s.dict["Filter"]).(type) {
	case pdfName:
		filters = []string{string(f)}
	case pdfArray:
		for _, x := range f {
			if n, ok := d.resolve(x).(pdfName); ok {
				filters = append(filters, string(n))
			}
		}
	}
	data := s.data
	for _, f := range filters {
		switch f {
		case "FlateDecode", "Fl":
			zr, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			out, err := io.ReadAll(zr)
			if len(out) == 0 && err != nil {
				return nil, err
			}
			data = out
		default:
			return nil, fmt.Errorf("unsupported filter %s", f)
		}
	}
	if parms := d.dict(s.dict["DecodeParms"]); parms != nil && parms.num("Predictor") > 1 {
		return nil, errors.New("unsupported predictor")
	}
	return data, nil
}

type pdfPage struct {
	dict      pdfDict
	resources pdfDict
}

func (d *pdfDoc) pages() []pdfPage {
	var root pdfDict
	for _, v := range d.objects {
		if dict := d.dict(v); dict != nil && dict.name("Type") == "Catalog" {
			root = dict
			break
		}
	}
	var out []pdfPage
	if root != nil {
		var walk func(node pdfDict, inherited pdfDict, depth int)
		walk = func(node pdfDict, inherited pdfDict, depth int) {
			if node == nil || depth > 32 {
				return
			}
			if r := d.dict(node["Resources"]); r != nil {
				inherited = r
			}
			if node.name("Type") == "Page" {
				out = append(out, pdfPage{dict: node, resources: inherited})
				return
			}
			if kids, ok := d.resolve(node["Kids"]).(pdfArray); ok {
				for _, k := range kids {
					walk(d.dict(k), inherited, depth+1)
				}
			}
		}
		walk(d.dict(root["Pages"]), nil, 0)
	}
	if len(out) > 0 {
		return out
	}
	nums := make([]int, 0, len(d.objects))
	for n := range d.objects {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		if dict := d.dict(d.objects[n]); dict != nil && dict.name("Type") == "Page" {
			out = append(out, pdfPage{dict: dict, resources: d.dict(dict["Resources"])})
		}
	}
	return out
}

func (d *pdfDoc) pageContent(page pdfDict) []byte {
	var parts []any
	switch c := d.resolve(page["Contents"]).(type) {
	case pdfArray:
		parts = c
	case pdfStream:
		parts = []any{c}
	}
	var buf bytes.Buffer
	for _, p := range parts {
		if s, ok := d.resolve(p).(pdfStream); ok {
			if data, err := d.decodeStream(s); err == nil {
				buf.Write(data)
				buf.WriteByte('\n')
			}
		}
	}
	return buf.Bytes()
}

// --- lexer -------------------------------------------------------------------

type pdfLexer struct {
	data []byte
	pos  int
}

func isPDFSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isPDFDelim(c byte) bool {
	return strings.IndexByte("()<>[]{}/%", c) >= 0
}

func (lx *pdfLexer) skipSpace() {
	for lx.pos < len(lx.data) {
		c := lx.data[lx.pos]
		if isPDFSpace(c) {
			lx.pos++
		} else if c == '%' {
			for lx.pos < len(lx.data) && lx.data[lx.pos] != '\n' && lx.data[lx.pos] != '\r' {
				lx.pos++
			}
		} else {
			return
		}
	}
}

var errPDFEOF = errors.New("unexpected end of PDF data")

// token returns the next token: a value, or a pdfKeyword for bare words and
// the delimiters "]" and ">>".
func (lx *pdfLexer) token() (any, error) {
	lx.skipSpace()
	if lx.pos >= len(lx.data) {
		return nil, errPDFEOF
	}
	c := lx.data[lx.pos]
	switch {
	case c == '/':
		lx.pos++
		start := lx.pos
		for lx.pos < len(lx.data) && !isPDFSpace(lx.data[lx.pos]) && !isPDFDelim(lx.data[lx.pos]) {
			lx.pos++
		}
		return pdfName(decodeNameEscapes(string(lx.data[start:lx.pos]))), nil
	case c == '(':
		return lx.literalString()
	case c == '<' && lx.pos+1 < len(lx.data) && lx.data[lx.pos+1] == '<':
		lx.pos += 2
		return pdfKeyword("<<"), nil
	case c == '>' && lx.pos+1 < len(lx.data) && lx.data[lx.pos+1] == '>':
		lx.pos += 2
		return pdfKeyword(">>"), nil
	case c == '<':
		return lx.hexString()
	case c == '[' || c == ']' || c == '{' || c == '}':
		lx.pos++
		return pdfKeyword(string(c)), nil
	case c == ')' || c == '>':
		lx.pos++
		return pdfKeyword(string(c)), nil
	}
	start := lx.pos
	for lx.pos < len(lx.data) && !isPDFSpace(lx.data[lx.pos]) && !isPDFDelim(lx.data[lx.pos]) {
		lx.pos++
	}
	word := string(lx.data[start:lx.pos])
	if f, err := strconv.ParseFloat(word, 64); err == nil {
		return f, nil
	}
	if strings.HasPrefix(word, "--") || strings.HasPrefix(word, "-.") {
		if f, err := strconv.ParseFloat(strings.TrimLeft(word, "-"), 64); err == nil {
			return -f, nil
		}
	}
	return pdfKeyword(word), nil
}

func decodeNameEscapes(s string) string {
	if !strings.Contains(s, "#") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (lx *pdfLexer) literalString() (any, error) {
	lx.pos++ // (
	var out []byte
	depth := 1
	for lx.pos < len(lx.data) {
		c := lx.data[lx.pos]
		lx.pos++
		switch c {
		case '(':
			depth++
			out = append(out, c)
		case ')':
			depth--
			if depth == 0 {
				return pdfString(out), nil
			}
			out = append(out, c)
		case '\\':
			if lx.pos >= len(lx.data) {
				return pdfString(out), nil
			}
			e := lx.data[lx.pos]
			lx.pos++
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if lx.pos < len(lx.data) && lx.data[lx.pos] == '\n' {
					lx.pos++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && lx.pos < len(lx.data) && lx.data[lx.pos] >= '0' && lx.data[lx.pos] <= '7'; k++ {
						v = v*8 + int(lx.data[lx.pos]-'0')
						lx.pos++
					}
					out = append(out, byte(v))
				} else {
					out = append(out, e)
				}
			}
		default:
			out = append(out, c)
		}
	}
	return pdfString(out), nil
}

func (lx *pdfLexer) hexString() (any, error) {
	lx.pos++ // <
	var digits []byte
	for lx.pos < len(lx.data) && lx.data[lx.pos] != '>' {
		c := lx.data[lx.pos]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			digits = append(digits, c)
		}
		lx.pos++
	}
	lx.pos++ // >
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	out := make([]byte, len(digits)/2)
	for i := range out {
		v, _ := strconv.ParseUint(string(digits[2*i:2*i+2]), 16, 8)
		out[i] = byte(v)
	}
	return pdfString(out), nil
}

// parseObject reads one value. With topLevel it also reads a following stream
// body. Indirect references ("12 0 R") are recognized.
func (lx *pdfLexer) parseObject(topLevel bool) (any, error) {
	tok, err := lx.token()
	if err != nil {
		return nil, err
	}
	v, err := lx.value(tok)
	if err != nil {
		return nil, err
	}
	if dict, ok := v.(pdfDict); ok && topLevel {
		save := lx.pos
		if next, err := lx.token(); err == nil && next == pdfKeyword("stream") {
			return lx.streamBody(dict), nil
		}
		lx.pos = save
	}
	return v, nil
}

func (lx *pdfLexer) value(tok any) (any, error) {
	switch t := tok.(type) {
	case pdfKeyword:
		switch t {
		case "<<":
			dict := pdfDict{}
			for {
				k, err := lx.token()
				if err != nil {
					return dict, nil
				}
				if k == pdfKeyword(">>") {
					return dict, nil
				}
				name, ok := k.(pdfName)
				if !ok {
					continue
				}
				vt, err := lx.token()
				if err != nil {
					return dict, nil
				}
				if vt == pdfKeyword(">>") {
					return dict, nil
				}
				val, err := lx.value(vt)
				if err != nil {
					return nil, err
				}
				dict[string(name)] = val
			}
		case "[":
			var arr pdfArray
			for {
				et, err := lx.token()
				if err != nil || et == pdfKeyword("]") {
					return arr, nil
				}
				val, err := lx.value(et)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		return t, nil
	case float64:
		// Maybe an indirect reference: int int R.
		save := lx.pos
		if t == math.Trunc(t) && t >= 0 {
			if g, err := lx.token(); err == nil {
				if gf, ok := g.(float64); ok && gf == math.Trunc(gf) && gf >= 0 {
					if r, err := lx.token(); err == nil && r == pdfKeyword("R") {
						return pdfRef{int(t), int(gf)}, nil
					}
				}
			}
		}
		lx.pos = save
		return t, nil
	}
	return tok, nil
}

func (lx *pdfLexer) streamBody(dict pdfDict) pdfStream {
	if lx.pos < len(lx.data) && lx.data[lx.pos] == '\r' {
		lx.pos++
	}
	if lx.pos < len(lx.data) && lx.data[lx.pos] == '\n' {
		lx.pos++
	}
	start := lx.pos
	if n, ok := dict["Length"].(float64); ok && n >= 0 && start+int(n) <= len(lx.data) {
		end := start + int(n)
		rest := bytes.TrimLeft(lx.data[end:min(end+32, len(lx.data))], "\r\n \t")
		if bytes.HasPrefix(rest, []byte("endstream")) {
			lx.pos = end
			return pdfStream{dict: dict, data: lx.data[start:end]}
		}
	}
	idx := bytes.Index(lx.data[start:], []byte("endstream"))
	if idx < 0 {
		lx.pos = len(lx.data)
		return pdfStream{dict: dict, data: lx.data[start:]}
	}
	end := start + idx
	lx.pos = end
	body := lx.data[start:end]
	body = bytes.TrimSuffix(body, []byte("\n"))
	body = bytes.TrimSuffix(body, []byte("\r"))
	return pdfStream{dict: dict, data: body}
}

func (d pdfDict) name(key string) string {
	n, _ := d[key].(pdfName)
	return string(n)
}

func (d pdfDict) num(key string) float64 {
	f, _ := d[key].(float64)
	return f
}

// --- fonts -------------------------------------------------------------------

type pdfFont struct {
	name      string
	twoByte   bool
	codeLens  []int // code lengths in bytes from the codespace ranges
	toUnicode map[string]string
	encoding  map[byte]rune
	widths    map[int]float64
	defaultW  float64
}

func (d *pdfDoc) loadFont(v any) *pdfFont {
	fd := d.dict(v)
	f := &pdfFont{widths: map[int]float64{}, defaultW: 500}
	if fd == nil {
		f.encoding = winAnsi()
		return f
	}
	f.name = fd.name("BaseFont")
	if i := strings.IndexByte(f.name, '+'); i == 6 {
		f.name = f.name[i+1:]
	}
	if fd.name("Subtype") == "Type0" {
		f.twoByte = true
		f.defaultW = 1000
		if arr, ok := d.resolve(fd["DescendantFonts"]).(pdfArray); ok && len(arr) > 0 {
			desc := d.dict(arr[0])
			if dw, ok := d.resolve(desc["DW"]).(float64); ok {
				f.defaultW = dw
			}
			if w, ok := d.resolve(desc["W"]).(pdfArray); ok {
				for i := 0; i < len(w); {
					first, ok := d.resolve(w[i]).(float64)
					if !ok || i+1 >= len(w) {
						break
					}
					if list, ok := d.resolve(w[i+1]).(pdfArray); ok {
						for j, x := range list {
							if xf, ok := d.resolve(x).(float64); ok {
								f.widths[int(first)+j] = xf
							}
						}
						i += 2
						continue
					}
					if i+2 >= len(w) {
						break
					}
					last, _ := d.resolve(w[i+1]).(float64)
					width, _ := d.resolve(w[i+2]).(float64)
					for c := int(first); c <= int(last) && c-int(first) < 65536; c++ {
						f.widths[c] = width
					}
					i += 3
				}
			}
		}
	} else {
		first := int(fd.num("FirstChar"))
		if w, ok := d.resolve(fd["Widths"]).(pdfArray); ok {
			for i, x := range w {
				if xf, ok := d.resolve(x).(float64); ok {
					f.widths[first+i] = xf
				}
			}
		}
		if desc := d.dict(fd["FontDescriptor"]); desc != nil {
			if mw := desc.num("MissingWidth"); mw > 0 {
				f.defaultW = mw
			}
		}
		f.encoding = d.simpleEncoding(fd["Encoding"])
	}
	if s, ok := d.resolve(fd["ToUnicode"]).(pdfStream); ok {
		if data, err := d.decodeStream(s); err == nil {
			f.toUnicode, f.codeLens = parseToUnicode(data)
		}
	}
	return f
}

func (d *pdfDoc) simpleEncoding(v any) map[byte]rune {
	enc := winAnsi()
	switch e := d.resolve(v).(type) {
	case pdfName:
		if e == "MacRomanEncoding" {
			enc = macRomanBasic()
		}
	case pdfDict:
		if e.name("BaseEncoding") == "MacRomanEncoding" {
			enc = macRomanBasic()
		}
		if diffs, ok := d.resolve(e["Differences"]).(pdfArray); ok {
			code := 0
			for _, x := range diffs {
				switch t := d.resolve(x).(type) {
				case float64:
					code = int(t)
				case pdfName:
					if r, ok := glyphRune(string(t)); ok && code >= 0 && code < 256 {
						enc[byte(code)] = r
					}
					code++
				}
			}
		}
	}
	return enc
}

// decode splits a shown string into codes and returns the text and the total
// advance width in text space units (thousandths), with the number of single
// byte space codes for word spacing.
func (f *pdfFont) decode(s []byte) (text string, width float64, spaces int) {
	var b strings.Builder
	for i := 0; i < len(s); {
		n := 1
		if f.twoByte {
			n = 2
		}
		if len(f.codeLens) > 0 {
			for _, l := range f.codeLens {
				if i+l <= len(s) {
					if _, ok := f.toUnicode[string(s[i:i+l])]; ok {
						n = l
						break
					}
				}
			}
		}
		if i+n > len(s) {
			n = len(s) - i
		}
		code := s[i : i+n]
		cid := 0
		for _, c := range code {
			cid = cid<<8 | int(c)
		}
		if u, ok := f.toUnicode[string(code)]; ok {
			b.WriteString(u)
		} else if !f.twoByte {
			if r, ok := f.encoding[code[0]]; ok {
				b.WriteRune(r)
			}
		}
		if w, ok := f.widths[cid]; ok {
			width += w
		} else {
			width += f.defaultW
		}
		if n == 1 && code[0] == 32 {
			spaces++
		}
		i += n
	}
	return b.String(), width, spaces
}

func parseToUnicode(data []byte) (map[string]string, []int) {
	out := map[string]string{}
	lens := map[int]bool{}
	lx := &pdfLexer{data: data}
	var mode string
	var ops []any
	for {
		tok, err := lx.token()
		if err != nil {
			break
		}
		if kw, ok := tok.(pdfKeyword); ok {
			switch kw {
			case "begincodespacerange", "beginbfchar", "beginbfrange":
				mode, ops = string(kw), nil
				continue
			case "[":
				v, _ := lx.value(tok)
				ops = append(ops, v)
				continue
			case "endcodespacerange":
				for i := 0; i+1 < len(ops); i += 2 {
					if lo, ok := ops[i].(pdfString); ok {
						lens[len(lo)] = true
					}
				}
				mode = ""
				continue
			case "endbfchar":
				for i := 0; i+1 < len(ops); i += 2 {
					src, ok1 := ops[i].(pdfString)
					dst, ok2 := ops[i+1].(pdfString)
					if ok1 && ok2 {
						out[string(src)] = utf16String(dst)
					}
				}
				mode = ""
				continue
			case "endbfrange":
				for i := 0; i+2 < len(ops); i += 3 {
					lo, ok1 := ops[i].(pdfString)
					hi, ok2 := ops[i+1].(pdfString)
					if !ok1 || !ok2 || len(lo) != len(hi) || len(lo) == 0 {
						continue
					}
					l, h := bytesToInt(lo), bytesToInt(hi)
					if h < l || h-l > 65535 {
						continue
					}
					switch dst := ops[i+2].(type) {
					case pdfString:
						base := []rune(utf16String(dst))
						for c := l; c <= h; c++ {
							r := append([]rune(nil), base...)
							if len(r) > 0 {
								r[len(r)-1] += rune(c - l)
							}
							out[string(intToBytes(c, len(lo)))] = string(r)
						}
					case pdfArray:
						for j, x := range dst {
							if s, ok := x.(pdfString); ok && l+j <= h {
								out[string(intToBytes(l+j, len(lo)))] = utf16String(s)
							}
						}
					}
				}
				mode = ""
				continue
			}
		}
		if mode != "" {
			ops = append(ops, tok)
		}
	}
	var codeLens []int
	for l := range lens {
		codeLens = append(codeLens, l)
	}
	sort.Ints(codeLens)
	return out, codeLens
}

func bytesToInt(b []byte) int {
	v := 0
	for _, c := range b {
		v = v<<8 | int(c)
	}
	return v
}

func intToBytes(v, n int) []byte {
	out := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		out[i] = byte(v)
		v >>= 8
	}
	return out
}

func utf16String(b []byte) string {
	if len(b)%2 == 1 {
		return string(b)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(u))
}

var winAnsiHigh = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡', 0x88: 'ˆ', 0x89: '‰',
	0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•',
	0x96: '–', 0x97: '—', 0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
}

func winAnsi() map[byte]rune {
	m := map[byte]rune{}
	for c := 32; c < 256; c++ {
		switch {
		case c < 127 || c >= 0xA0:
			m[byte(c)] = rune(c)
		case winAnsiHigh[byte(c)] != 0:
			m[byte(c)] = winAnsiHigh[byte(c)]
		}
	}
	return m
}

// macRomanBasic covers ASCII; card text outside it comes through ToUnicode.
func macRomanBasic() map[byte]rune {
	m := map[byte]rune{}
	for c := 32; c < 127; c++ {
		m[byte(c)] = rune(c)
	}
	return m
}

var glyphNames = map[string]rune{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#', "dollar": '$', "percent": '%',
	"ampersand": '&', "quotesingle": '\'', "parenleft": '(', "parenright": ')', "asterisk": '*',
	"plus": '+', "comma": ',', "hyphen": '-', "period": '.', "slash": '/', "zero": '0', "one": '1',
	"two": '2', "three": '3', "four": '4', "five": '5', "six": '6', "seven": '7', "eight": '8',
	"nine": '9', "colon": ':', "semicolon": ';', "less": '<', "equal": '=', "greater": '>',
	"question": '?', "at": '@', "bracketleft": '[', "backslash": '\\', "bracketright": ']',
	"underscore": '_', "bar": '|', "quoteleft": '‘', "quoteright": '’', "quotedblleft": '“',
	"quotedblright": '”', "endash": '–', "emdash": '—', "bullet": '•', "degree": '°',
	"onehalf": '½', "onequarter": '¼', "threequarters": '¾', "fraction": '⁄', "ellipsis": '…',
	"registered": '®', "copyright": '©', "trademark": '™', "eacute": 'é', "egrave": 'è',
	"aacute": 'á', "iacute": 'í', "oacute": 'ó', "uacute": 'ú', "ntilde": 'ñ', "ccedilla": 'ç',
	"fi": 'ﬁ', "fl": 'ﬂ', "minus": '−', "multiply": '×', "nbspace": ' ',
}

func glyphRune(name string) (rune, bool) {
	if len(name) == 1 {
		return rune(name[0]), true
	}
	if r, ok := glyphNames[name]; ok {
		return r, true
	}
	if strings.HasPrefix(name, "uni") && len(name) == 7 {
		if v, err := strconv.ParseUint(name[3:], 16, 32); err == nil {
			return rune(v), true
		}
	}
	return 0, false
}

// --- content interpretation ---------------------------------------------------

type matrix [6]float64

var identityMatrix = matrix{1, 0, 0, 1, 0, 0}

func (m matrix) mul(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

type textState struct {
	font                        *pdfFont
	size, charSp, wordSp, scale float64
	leading, rise               float64
}

type contentInterpreter struct {
	doc   *pdfDoc
	page  int
	fonts map[string]*pdfFont
	runs  []TextRun
}

func (in *contentInterpreter) fontFor(res pdfDict, name string) *pdfFont {
	fonts := in.doc.dict(res["Font"])
	ref := fonts[name]
	key := fmt.Sprintf("%v", ref)
	if r, ok := ref.(pdfRef); ok {
		key = fmt.Sprintf("ref%d", r.num)
	}
	if f, ok := in.fonts[key]; ok {
		return f
	}
	f := in.doc.loadFont(ref)
	in.fonts[key] = f
	return f
}

func (in *contentInterpreter) run(content []byte, res pdfDict, ctm matrix, depth int) {
	if depth > 8 {
		return
	}
	lx := &pdfLexer{data: content}
	var operands []any
	var stack []matrix
	var tsStack []textState
	ts := textState{scale: 1}
	tm, tlm := identityMatrix, identityMatrix

	// show places a TJ array (a Tj string is a one-element array) as one run,
	// turning wide negative adjustments (word gaps) into spaces.
	show := func(parts pdfArray) {
		if ts.font == nil {
			ts.font = in.doc.loadFont(nil)
		}
		trm := matrix{ts.size * ts.scale, 0, 0, ts.size, 0, ts.rise}.mul(tm).mul(ctm)
		var b strings.Builder
		for _, el := range parts {
			switch t := el.(type) {
			case pdfString:
				text, w, spaces := ts.font.decode(t)
				b.WriteString(text)
				tx := (w/1000*ts.size + ts.charSp*float64(len(t)) + ts.wordSp*float64(spaces)) * ts.scale
				tm = matrix{1, 0, 0, 1, tx, 0}.mul(tm)
			case float64:
				tm = matrix{1, 0, 0, 1, -t / 1000 * ts.size * ts.scale, 0}.mul(tm)
				if t < -180 && b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
					b.WriteByte(' ')
				}
			}
		}
		text := b.String()
		end := tm.mul(ctm)
		size := ts.size * math.Hypot(end[2], end[3])
		if strings.TrimSpace(text) == "" {
			// Keep spaces attached to the previous run on the same line.
			if n := len(in.runs); n > 0 && math.Abs(in.runs[n-1].Y-trm[5]) < 0.5 && in.runs[n-1].Page == in.page {
				in.runs[n-1].Text += text
				in.runs[n-1].EndX = end[4]
			}
			return
		}
		in.runs = append(in.runs, TextRun{Page: in.page, X: trm[4], Y: trm[5], EndX: end[4], Size: size, Font: ts.font.name, Text: text})
	}

	for {
		tok, err := lx.token()
		if err != nil {
			return
		}
		kw, isKW := tok.(pdfKeyword)
		if !isKW || kw == "[" || kw == "<<" {
			v, err := lx.value(tok)
			if err != nil {
				return
			}
			operands = append(operands, v)
			continue
		}
		num := func(i int) float64 {
			if i < len(operands) {
				f, _ := operands[i].(float64)
				return f
			}
			return 0
		}
		switch kw {
		case "q":
			stack = append(stack, ctm)
			tsStack = append(tsStack, ts)
		case "Q":
			if n := len(stack); n > 0 {
				ctm, stack = stack[n-1], stack[:n-1]
				ts, tsStack = tsStack[n-1], tsStack[:n-1]
			}
		case "cm":
			if len(operands) >= 6 {
				ctm = matrix{num(0), num(1), num(2), num(3), num(4), num(5)}.mul(ctm)
			}
		case "BT":
			tm, tlm = identityMatrix, identityMatrix
		case "Tf":
			if len(operands) >= 2 {
				if n, ok := operands[0].(pdfName); ok {
					ts.font = in.fontFor(res, string(n))
				}
				ts.size = num(1)
			}
		case "Tc":
			ts.charSp = num(0)
		case "Tw":
			ts.wordSp = num(0)
		case "Tz":
			ts.scale = num(0) / 100
		case "TL":
			ts.leading = num(0)
		case "Ts":
			ts.rise = num(0)
		case "Td":
			tlm = matrix{1, 0, 0, 1, num(0), num(1)}.mul(tlm)
			tm = tlm
		case "TD":
			ts.leading = -num(1)
			tlm = matrix{1, 0, 0, 1, num(0), num(1)}.mul(tlm)
			tm = tlm
		case "Tm":
			if len(operands) >= 6 {
				tlm = matrix{num(0), num(1), num(2), num(3), num(4), num(5)}
				tm = tlm
			}
		case "T*":
			tlm = matrix{1, 0, 0, 1, 0, -ts.leading}.mul(tlm)
			tm = tlm
		case "Tj":
			if n := len(operands); n > 0 {
				if str, ok := operands[n-1].(pdfString); ok {
					show(pdfArray{str})
				}
			}
		case "'", "\"":
			if kw == "\"" && len(operands) >= 3 {
				ts.wordSp, ts.charSp = num(0), num(1)
			}
			tlm = matrix{1, 0, 0, 1, 0, -ts.leading}.mul(tlm)
			tm = tlm
			if n := len(operands); n > 0 {
				if str, ok := operands[n-1].(pdfString); ok {
					show(pdfArray{str})
				}
			}
		case "TJ":
			if n := len(operands); n > 0 {
				if arr, ok := operands[n-1].(pdfArray); ok {
					show(arr)
				}
			}
		case "Do":
			if len(operands) >= 1 {
				if n, ok := operands[0].(pdfName); ok {
					xobjs := in.doc.dict(res["XObject"])
					if s, ok := in.doc.resolve(xobjs[string(n)]).(pdfStream); ok && s.dict.name("Subtype") == "Form" {
						data, err := in.doc.decodeStream(s)
						if err == nil {
							m := identityMatrix
							if arr, ok := in.doc.resolve(s.dict["Matrix"]).(pdfArray); ok && len(arr) == 6 {
								for i := range m {
									m[i], _ = in.doc.resolve(arr[i]).(float64)
								}
							}
							sub := in.doc.dict(s.dict["Resources"])
							if sub == nil {
								sub = res
							}
							in.run(data, sub, m.mul(ctm), depth+1)
						}
					}
				}
			}
		case "BI":
			// Skip inline image data up to EI.
			if idx := bytes.Index(lx.data[lx.pos:], []byte("ID")); idx >= 0 {
				lx.pos += idx + 2
				if e := bytes.Index(lx.data[lx.pos:], []byte("EI")); e >= 0 {
					lx.pos += e + 2
				}
			}
		}
		operands = operands[:0]
	}
}
