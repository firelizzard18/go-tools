package testfuncs

func (x *Context) bind(expr Expression) Expression {
	for ref := range expr.Needs() {
		if _, ok := x.Values[ref]; ok {
			continue
		}

		// TODO: Resolve

		// Unresolvable ref
		return expr
	}

	return expr.Bind(x.Values)
}
