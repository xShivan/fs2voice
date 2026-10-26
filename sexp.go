package main

// Minimal S-expression parser for FreeSpace 2 mission SEXPs.
//
// FS2 SEXPs are fully parenthesized, whitespace-insensitive, and use
// double-quoted strings for text arguments (no escape sequences, and no
// XSTR() wrappers inside #Events). Bare atoms are runs of non-space,
// non-paren, non-quote characters and cover operator names, numbers and
// SEXP-variable references such as @foo[0].

// node is either a list (Items != nil) or an atom (Items == nil).
type node struct {
	Items  []*node // non-nil => list
	Value  string  // atom text (unquoted content for quoted atoms)
	Quoted bool    // atom was a "..." string
	Line   int     // 1-based file line where the token starts
	Col    int     // 0-based byte column in that line where the token starts
}

func (n *node) isList() bool { return n.Items != nil }

// head returns the operator name of a list node, or "" for atoms / empty lists.
func (n *node) head() string {
	if n.isList() && len(n.Items) > 0 && !n.Items[0].isList() {
		return n.Items[0].Value
	}
	return ""
}

// parseSexps parses every top-level S-expression found in src. startLine and
// startCol are the 1-based file line and 0-based byte column of the first
// character of src, used to attribute nodes to file positions. Malformed /
// unbalanced input is tolerated: parsing stops at the first point it can no
// longer make progress.
func parseSexps(src string, startLine, startCol int) []*node {
	p := &sexpParser{src: src, line: startLine, colBase: startCol}
	var out []*node
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		if p.src[p.pos] != '(' {
			// Skip stray junk between top-level forms.
			p.advance()
			continue
		}
		n := p.parseForm()
		if n == nil {
			break
		}
		out = append(out, n)
	}
	return out
}

type sexpParser struct {
	src       string
	pos       int
	line      int
	lineStart int // pos of the first character of the current line
	colBase   int // column offset of the first line (src may start mid-line)
}

func (p *sexpParser) advance() {
	if p.pos < len(p.src) {
		if p.src[p.pos] == '\n' {
			p.line++
			p.lineStart = p.pos + 1
			p.colBase = 0
		}
		p.pos++
	}
}

func (p *sexpParser) col() int { return p.colBase + p.pos - p.lineStart }

func (p *sexpParser) skipSpace() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.advance()
			continue
		}
		// FS2 line comments: ';' to end of line (also ';!' hints).
		if c == ';' {
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.advance()
			}
			continue
		}
		return
	}
}

// parseForm parses a single '(' ... ')' list. Assumes current char is '('.
func (p *sexpParser) parseForm() *node {
	list := &node{Items: []*node{}, Line: p.line, Col: p.col()}
	p.advance() // consume '('
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return list // unbalanced; return what we have
		}
		c := p.src[p.pos]
		switch c {
		case ')':
			p.advance()
			return list
		case '(':
			child := p.parseForm()
			if child == nil {
				return list
			}
			list.Items = append(list.Items, child)
		case '"':
			list.Items = append(list.Items, p.parseString())
		default:
			list.Items = append(list.Items, p.parseAtom())
		}
	}
}

func (p *sexpParser) parseString() *node {
	startLine, startCol := p.line, p.col()
	p.advance() // consume opening quote
	var b []byte
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '"' {
			p.advance()
			break
		}
		b = append(b, c)
		p.advance()
	}
	return &node{Value: string(b), Quoted: true, Line: startLine, Col: startCol}
}

func (p *sexpParser) parseAtom() *node {
	startLine, startCol := p.line, p.col()
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '(' || c == ')' || c == '"' || c == ';' {
			break
		}
		p.pos++
	}
	return &node{Value: p.src[start:p.pos], Quoted: false, Line: startLine, Col: startCol}
}

// walk visits n and every descendant list node, depth-first, pre-order.
func walk(n *node, fn func(*node)) {
	if n == nil {
		return
	}
	if n.isList() {
		fn(n)
		for _, c := range n.Items {
			walk(c, fn)
		}
	}
}
