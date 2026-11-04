package draft

// Clone returns a deep copy of d. Nil slices stay nil and empty slices stay empty, so a clone
// compares equal (reflect.DeepEqual) to the original.
func (d Draft) Clone() Draft {
	d.Content = d.Content.Clone()
	return d
}

// Clone returns a deep copy of c that shares no slice or pointer with it.
func (c Content) Clone() Content {
	c.Ingredients = cloneSlice(c.Ingredients)
	c.Instructions = cloneSlice(c.Instructions)
	for i := range c.Instructions {
		c.Instructions[i].PhaseBasis = cloneBasis(c.Instructions[i].PhaseBasis)
	}
	c.Sources = cloneSlice(c.Sources)
	c.Estimates = cloneEstimates(c.Estimates)
	if c.PhaseEstimates != nil {
		pe := *c.PhaseEstimates
		pe.PrepAhead = cloneEstimates(pe.PrepAhead)
		pe.PrepTonight = cloneEstimates(pe.PrepTonight)
		pe.CookTonight = cloneEstimates(pe.CookTonight)
		c.PhaseEstimates = &pe
	}
	c.Guidance = cloneSlice(c.Guidance)
	for i := range c.Guidance {
		c.Guidance[i].Basis = cloneBasis(c.Guidance[i].Basis)
	}
	return c
}

func cloneSlice[T any](s []T) []T {
	if s == nil {
		return nil
	}
	return append(make([]T, 0, len(s)), s...)
}

func cloneBasis(b *Basis) *Basis {
	if b == nil {
		return nil
	}
	c := *b
	return &c
}

func cloneEstimates(e *Estimates) *Estimates {
	if e == nil {
		return nil
	}
	c := *e
	if e.ActiveMinutes != nil {
		m := *e.ActiveMinutes
		if m.Value != nil {
			v := *m.Value
			m.Value = &v
		}
		m.Basis = cloneBasis(m.Basis)
		c.ActiveMinutes = &m
	}
	if e.TotalMinutes != nil {
		m := *e.TotalMinutes
		if m.Value != nil {
			v := *m.Value
			m.Value = &v
		}
		m.Basis = cloneBasis(m.Basis)
		c.TotalMinutes = &m
	}
	c.Attention = cloneLevel(e.Attention)
	c.Cleanup = cloneLevel(e.Cleanup)
	c.Variability = cloneLevel(e.Variability)
	return &c
}

func cloneLevel(l *LevelEstimate) *LevelEstimate {
	if l == nil {
		return nil
	}
	c := *l
	c.Basis = cloneBasis(l.Basis)
	return &c
}
