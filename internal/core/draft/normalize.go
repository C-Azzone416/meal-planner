package draft

// normalize returns a deep copy of c in the canonical stored form: list fields are never nil, and
// an estimates object with nothing in it is dropped (it means the same as no estimates).
func normalize(c Content) Content {
	c = c.Clone()
	if c.Ingredients == nil {
		c.Ingredients = []Ingredient{}
	}
	if c.Instructions == nil {
		c.Instructions = []Step{}
	}
	if c.Sources == nil {
		c.Sources = []Source{}
	}
	if c.Guidance == nil {
		c.Guidance = []Guidance{}
	}
	if emptyEstimates(c.Estimates) {
		c.Estimates = nil
	}
	if pe := c.PhaseEstimates; pe != nil {
		if emptyEstimates(pe.PrepAhead) {
			pe.PrepAhead = nil
		}
		if emptyEstimates(pe.PrepTonight) {
			pe.PrepTonight = nil
		}
		if emptyEstimates(pe.CookTonight) {
			pe.CookTonight = nil
		}
		if pe.PrepAhead == nil && pe.PrepTonight == nil && pe.CookTonight == nil {
			c.PhaseEstimates = nil
		}
	}
	return c
}

func emptyEstimates(e *Estimates) bool {
	return e == nil || (e.ActiveMinutes == nil && e.TotalMinutes == nil && e.Attention == nil && e.Cleanup == nil && e.Variability == nil)
}
