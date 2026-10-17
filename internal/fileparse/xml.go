package fileparse

import (
	"encoding/xml"
	"strings"
)

// xmlNode 通用 XML 节点（容忍未知结构，按"重复出现的元素=记录"提取）。
type xmlNode struct {
	Name     string
	Attrs    []kvPair
	Text     string
	Children []*xmlNode
	Depth    int
}

type kvPair struct{ K, V string }

// parseXML 通用 XML 解析：取出现次数最多（且带子元素/属性）的元素作为记录元素。
func parseXML(text string) Result {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var root *xmlNode
	var stack []*xmlNode
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: t.Name.Local, Depth: len(stack)}
			for _, a := range t.Attr {
				n.Attrs = append(n.Attrs, kvPair{K: a.Name.Local, V: strings.TrimSpace(a.Value)})
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return Result{Error: "XML 解析失败：未找到根元素"}
	}

	recs := pickRecordNodes(root)
	if len(recs) == 0 {
		return Result{Error: "XML 解析失败：未找到可识别的记录元素"}
	}
	res := Result{Note: "记录元素 <" + recs[0].Name + ">"}
	for _, n := range recs {
		row := Record{}
		for _, a := range n.Attrs {
			row[a.K] = a.V
		}
		for _, c := range n.Children {
			v := strings.TrimSpace(c.Text)
			if v == "" && len(c.Attrs) > 0 {
				parts := make([]string, 0, len(c.Attrs))
				for _, a := range c.Attrs {
					parts = append(parts, a.K+"="+a.V)
				}
				v = strings.Join(parts, " ")
			}
			row[c.Name] = v
		}
		if len(row) == 0 {
			if v := strings.TrimSpace(n.Text); v != "" {
				row["值"] = v
			}
		}
		if len(row) > 0 {
			res.Rows = append(res.Rows, row)
			for k := range row {
				if !containsStr(res.Columns, k) {
					res.Columns = append(res.Columns, k)
				}
			}
		}
	}
	return res
}

// pickRecordNodes 选取记录元素：出现次数最多的（同数量时取层级更深者）。
func pickRecordNodes(root *xmlNode) []*xmlNode {
	counts := map[string][]*xmlNode{}
	var walk func(n *xmlNode)
	walk = func(n *xmlNode) {
		for _, c := range n.Children {
			if len(c.Children) > 0 || len(c.Attrs) > 0 {
				counts[c.Name] = append(counts[c.Name], c)
			}
			walk(c)
		}
	}
	walk(root)
	best := ""
	bestLen := 0
	bestDepth := -1
	for name, nodes := range counts {
		if len(nodes) > bestLen || (len(nodes) == bestLen && nodes[0].Depth > bestDepth) {
			best, bestLen, bestDepth = name, len(nodes), nodes[0].Depth
		}
	}
	if best == "" {
		return nil
	}
	return counts[best]
}
