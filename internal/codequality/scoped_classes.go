package codequality

func scopedClassFinding(m *metrics, f File, text, name, rule string, start, open, value, limit int, pairs map[int]int) {
	if value <= limit {
		return
	}
	m.add(f.Path, m.lineAt(start), rule, name, value, limit)
	m.findings[len(m.findings)-1].key += "\x00" + scriptIdentity(m.ctx, f.Text, text, open, open, pairs)
}
