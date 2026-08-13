package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ganigeorgiev/fexpr"
	"github.com/hanzoai/base/tools/security"
	"github.com/hanzoai/base/tools/store"
	"github.com/hanzoai/orm/dialect"
	"github.com/hanzoai/orm/query"
	"github.com/spf13/cast"
)

// FilterData is a filter expression string following the `fexpr` package grammar.
//
// The filter string can also contain dbx placeholder parameters (eg. "title = {:name}"),
// that will be safely replaced and properly quoted inplace with the placeholderReplacements values.
//
// Example:
//
//	var filter FilterData = "id = null || (name = 'test' && status = true) || (total >= {:min} && total <= {:max})"
//	resolver := search.NewSimpleFieldResolver("id", "name", "status")
//	expr, err := filter.BuildExpr(resolver, query.Params{"min": 100, "max": 200})
type FilterData string

// parsedFilterData holds a cache with previously parsed filter data expressions
// (initialized with some preallocated empty data map)
var parsedFilterData = store.New(make(map[string][]fexpr.ExprGroup, 50))

// BuildExpr parses the current filter data and returns a new db WHERE expression.
//
// The filter string can also contain dbx placeholder parameters (eg. "title = {:name}"),
// that will be safely replaced and properly quoted inplace with the placeholderReplacements values.
//
// The parsed expressions are limited up to DefaultFilterExprLimit.
// Use [FilterData.BuildExprWithLimit] if you want to set a custom limit.
func (f FilterData) BuildExpr(
	fieldResolver FieldResolver,
	placeholderReplacements ...query.Params,
) (query.Expression, error) {
	return f.BuildExprWithLimit(fieldResolver, DefaultFilterExprLimit, placeholderReplacements...)
}

// BuildExpr parses the current filter data and returns a new db WHERE expression.
//
// The filter string can also contain dbx placeholder parameters (eg. "title = {:name}"),
// that will be safely replaced and properly quoted inplace with the placeholderReplacements values.
func (f FilterData) BuildExprWithLimit(
	fieldResolver FieldResolver,
	maxExpressions int,
	placeholderReplacements ...query.Params,
) (query.Expression, error) {
	raw := string(f)

	// replace the placeholder params in the raw string filter
	for _, p := range placeholderReplacements {
		for key, value := range p {
			var replacement string
			switch v := value.(type) {
			case nil:
				replacement = "null"
			case bool, float64, float32, int, int64, int32, int16, int8, uint, uint64, uint32, uint16, uint8:
				replacement = cast.ToString(v)
			default:
				replacement = cast.ToString(v)

				// try to json serialize as fallback
				if replacement == "" {
					raw, _ := json.Marshal(v)
					replacement = string(raw)
				}

				replacement = strconv.Quote(replacement)
			}
			raw = strings.ReplaceAll(raw, "{:"+key+"}", replacement)
		}
	}

	cacheKey := raw + "/" + strconv.Itoa(maxExpressions)

	if data, ok := parsedFilterData.GetOk(cacheKey); ok {
		return buildParsedFilterExpr(data, fieldResolver, &maxExpressions)
	}

	data, err := fexpr.Parse(raw)
	if err != nil {
		// depending on the users demand we may allow empty expressions
		// (aka. expressions consisting only of whitespaces or comments)
		// but for now disallow them as it seems unnecessary
		// if errors.Is(err, fexpr.ErrEmpty) {
		// return query.NewExp("1=1"), nil
		// }

		return nil, err
	}

	// store in cache
	// (the limit size is arbitrary and it is there to prevent the cache growing too big)
	parsedFilterData.SetIfLessThanLimit(cacheKey, data, 500)

	return buildParsedFilterExpr(data, fieldResolver, &maxExpressions)
}

func buildParsedFilterExpr(data []fexpr.ExprGroup, fieldResolver FieldResolver, maxExpressions *int) (query.Expression, error) {
	if len(data) == 0 {
		return nil, fexpr.ErrEmpty
	}

	result := &concatExpr{separator: " "}

	for _, group := range data {
		var expr query.Expression
		var exprErr error

		switch item := group.Item.(type) {
		case fexpr.Expr:
			if *maxExpressions <= 0 {
				return nil, ErrFilterExprLimit
			}

			*maxExpressions--

			expr, exprErr = resolveTokenizedExpr(item, fieldResolver)
		case fexpr.ExprGroup:
			expr, exprErr = buildParsedFilterExpr([]fexpr.ExprGroup{item}, fieldResolver, maxExpressions)
		case []fexpr.ExprGroup:
			expr, exprErr = buildParsedFilterExpr(item, fieldResolver, maxExpressions)
		default:
			exprErr = errors.New("unsupported expression item")
		}

		if exprErr != nil {
			return nil, exprErr
		}

		if len(result.parts) > 0 {
			var op string
			if group.Join == fexpr.JoinOr {
				op = "OR"
			} else {
				op = "AND"
			}
			result.parts = append(result.parts, &opExpr{op})
		}

		result.parts = append(result.parts, expr)
	}

	return result, nil
}

func resolveTokenizedExpr(expr fexpr.Expr, fieldResolver FieldResolver) (query.Expression, error) {
	lResult, lErr := resolveToken(expr.Left, fieldResolver)
	if lErr != nil || lResult.Identifier == "" {
		return nil, fmt.Errorf("invalid left operand %q - %v", expr.Left.Literal, lErr)
	}

	rResult, rErr := resolveToken(expr.Right, fieldResolver)
	if rErr != nil || rResult.Identifier == "" {
		return nil, fmt.Errorf("invalid right operand %q - %v", expr.Right.Literal, rErr)
	}

	return buildResolversExpr(fieldResolver.Dialect(), lResult, expr.Op, rResult)
}

func buildResolversExpr(
	d dialect.Dialect,
	left *ResolverResult,
	op fexpr.SignOp,
	right *ResolverResult,
) (query.Expression, error) {
	var expr query.Expression

	// A value read out of a JSON document arrives as text whatever the
	// document held, so where it is compared to a number it is read as one. A
	// pattern match is left as it is — it matches text on either side.
	if !isLikeOp(op) {
		left, right = readAsNumbers(d, left, right)
	}

	switch op {
	case fexpr.SignEq, fexpr.SignAnyEq:
		expr = resolveEqualExpr(true, left, right)
	case fexpr.SignNeq, fexpr.SignAnyNeq:
		expr = resolveEqualExpr(false, left, right)
	case fexpr.SignLike, fexpr.SignAnyLike:
		// the right side is a column and therefor wrap it with "%" for contains like behavior
		if len(right.Params) == 0 {
			expr = query.NewExp(fmt.Sprintf("%s %s ('%%' || %s || '%%') ESCAPE '\\'", left.Identifier, d.Like(), right.Identifier), left.Params)
		} else {
			expr = query.NewExp(fmt.Sprintf("%s %s %s ESCAPE '\\'", left.Identifier, d.Like(), right.Identifier), mergeParams(left.Params, wrapLikeParams(right.Params)))
		}
	case fexpr.SignNlike, fexpr.SignAnyNlike:
		// the right side is a column and therefor wrap it with "%" for not-contains like behavior
		if len(right.Params) == 0 {
			expr = query.NewExp(fmt.Sprintf("%s NOT %s ('%%' || %s || '%%') ESCAPE '\\'", left.Identifier, d.Like(), right.Identifier), left.Params)
		} else {
			expr = query.NewExp(fmt.Sprintf("%s NOT %s %s ESCAPE '\\'", left.Identifier, d.Like(), right.Identifier), mergeParams(left.Params, wrapLikeParams(right.Params)))
		}
	case fexpr.SignLt, fexpr.SignAnyLt:
		expr = query.NewExp(fmt.Sprintf("%s < %s", left.Identifier, right.Identifier), mergeParams(left.Params, right.Params))
	case fexpr.SignLte, fexpr.SignAnyLte:
		expr = query.NewExp(fmt.Sprintf("%s <= %s", left.Identifier, right.Identifier), mergeParams(left.Params, right.Params))
	case fexpr.SignGt, fexpr.SignAnyGt:
		expr = query.NewExp(fmt.Sprintf("%s > %s", left.Identifier, right.Identifier), mergeParams(left.Params, right.Params))
	case fexpr.SignGte, fexpr.SignAnyGte:
		expr = query.NewExp(fmt.Sprintf("%s >= %s", left.Identifier, right.Identifier), mergeParams(left.Params, right.Params))
	}

	if expr == nil {
		return nil, fmt.Errorf("unknown expression operator %q", op)
	}

	// multi-match expressions
	if !isAnyMatchOp(op) {
		mm, err := multiMatch(d, left, op, right)
		if err != nil {
			return nil, err
		}

		if mm != nil {
			expr = query.Enclose(query.And(expr, mm))
		}
	}

	if left.AfterBuild != nil {
		expr = left.AfterBuild(expr)
	}

	if right.AfterBuild != nil {
		expr = right.AfterBuild(expr)
	}

	return expr, nil
}

// normalizedIdentifier spells the three words that name a value rather than a
// field, for a collection that has no field by that name. The truth values are
// the engine's own, which is not the same word on every engine.
func normalizedIdentifier(d dialect.Dialect, literal string) (string, bool) {
	switch strings.ToLower(literal) {
	case "null":
		return "NULL", true
	case "true":
		return d.Bool(true), true
	case "false":
		return d.Bool(false), true
	}

	return "", false
}

func resolveToken(token fexpr.Token, fieldResolver FieldResolver) (*ResolverResult, error) {
	switch token.Type {
	case fexpr.TokenIdentifier:
		// check for macros
		// ---
		if macroFunc, ok := identifierMacros[token.Literal]; ok {
			placeholder := "t" + security.PseudorandomString(8)

			macroValue, err := macroFunc()
			if err != nil {
				return nil, err
			}

			return &ResolverResult{
				Identifier: "{:" + placeholder + "}",
				Params:     query.Params{placeholder: macroValue},
			}, nil
		}

		// custom resolver
		// ---
		result, err := fieldResolver.Resolve(token.Literal)
		if err != nil || result.Identifier == "" {
			if identifier, ok := normalizedIdentifier(fieldResolver.Dialect(), token.Literal); ok {
				return &ResolverResult{Identifier: identifier}, nil
			}
			return nil, err
		}

		return result, err
	case fexpr.TokenText:
		placeholder := "t" + security.PseudorandomString(8)

		return &ResolverResult{
			Identifier: "{:" + placeholder + "}",
			Params:     query.Params{placeholder: token.Literal},
		}, nil
	case fexpr.TokenNumber:
		placeholder := "t" + security.PseudorandomString(8)

		return &ResolverResult{
			Identifier: "{:" + placeholder + "}",
			Params:     query.Params{placeholder: cast.ToFloat64(token.Literal)},
		}, nil
	case fexpr.TokenFunction:
		fn, ok := TokenFunctions[token.Literal]
		if !ok {
			return nil, fmt.Errorf("unknown function %q", token.Literal)
		}

		args, _ := token.Meta.([]fexpr.Token)
		return fn(fieldResolver.Dialect(), func(argToken fexpr.Token) (*ResolverResult, error) {
			return resolveToken(argToken, fieldResolver)
		}, args...)
	}

	return nil, fmt.Errorf("unsupported token type %q", token.Type)
}

// Resolves = and != expressions in an attempt to minimize the COALESCE
// usage and to gracefully handle null vs empty string normalizations.
//
// The expression `a = "" OR a is null` tends to perform better than
// `COALESCE(a, "") = ""` since the direct match can be accomplished
// with a seek while the COALESCE will induce a table scan.
func resolveEqualExpr(equal bool, left, right *ResolverResult) query.Expression {
	equalOp := "="
	nullEqualOp := "IS NOT DISTINCT FROM"
	concatOp := "OR"
	nullExpr := "IS NULL"
	if !equal {
		// always use the null-safe form instead of `!=` because a direct
		// non-equal comparison to a column that is actually NULL yields NULL
		// rather than TRUE, eg.:
		// `'example' != nullableColumn` -> NULL even if nullableColumn row value is NULL
		equalOp = "IS DISTINCT FROM"
		nullEqualOp = equalOp
		concatOp = "AND"
		nullExpr = "IS NOT NULL"
	}

	// no coalesce fallback (eg. compare to a json field)
	// a IS b
	// a IS NOT b
	if left.NullFallback == NullFallbackDisabled ||
		right.NullFallback == NullFallbackDisabled {
		return query.NewExp(
			fmt.Sprintf("%s %s %s", left.Identifier, nullEqualOp, right.Identifier),
			mergeParams(left.Params, right.Params),
		)
	}

	isLeftEmpty := isEmptyIdentifier(left) ||
		(left.NullFallback == NullFallbackAuto && len(left.Params) == 1 && hasEmptyParamValue(left))

	isRightEmpty := isEmptyIdentifier(right) ||
		(right.NullFallback == NullFallbackAuto && len(right.Params) == 1 && hasEmptyParamValue(right))

	// both operands are empty
	if isLeftEmpty && isRightEmpty {
		return query.NewExp(fmt.Sprintf("'' %s ''", equalOp), mergeParams(left.Params, right.Params))
	}

	// direct compare since at least one of the operands is known to be non-empty
	// eg. a = 'example'
	if isKnownNonEmptyIdentifier(left) || isKnownNonEmptyIdentifier(right) {
		leftIdentifier := left.Identifier
		if isLeftEmpty {
			leftIdentifier = "''"
		}
		rightIdentifier := right.Identifier
		if isRightEmpty {
			rightIdentifier = "''"
		}
		return query.NewExp(
			fmt.Sprintf("%s %s %s", leftIdentifier, equalOp, rightIdentifier),
			mergeParams(left.Params, right.Params),
		)
	}

	// "" = b OR b IS NULL
	// "" IS NOT b AND b IS NOT NULL
	if isLeftEmpty {
		return query.NewExp(
			fmt.Sprintf("('' %s %s %s %s %s)", equalOp, right.Identifier, concatOp, right.Identifier, nullExpr),
			mergeParams(left.Params, right.Params),
		)
	}

	// a = "" OR a IS NULL
	// a IS NOT "" AND a IS NOT NULL
	if isRightEmpty {
		return query.NewExp(
			fmt.Sprintf("(%s %s '' %s %s %s)", left.Identifier, equalOp, concatOp, left.Identifier, nullExpr),
			mergeParams(left.Params, right.Params),
		)
	}

	// fallback to a COALESCE comparison
	return query.NewExp(
		fmt.Sprintf(
			"COALESCE(%s, '') %s COALESCE(%s, '')",
			left.Identifier,
			equalOp,
			right.Identifier,
		),
		mergeParams(left.Params, right.Params),
	)
}

func hasEmptyParamValue(result *ResolverResult) bool {
	for _, p := range result.Params {
		switch v := p.(type) {
		case nil:
			return true
		case string:
			if v == "" {
				return true
			}
		}
	}

	return false
}

func isKnownNonEmptyIdentifier(result *ResolverResult) bool {
	if result.NullFallback == NullFallbackEnforced {
		return false
	}

	switch strings.ToLower(result.Identifier) {
	case "1", "0", "false", `true`, `'false'`, `'true'`:
		return true
	}

	return len(result.Params) > 0 && !hasEmptyParamValue(result) && !isEmptyIdentifier(result)
}

func isEmptyIdentifier(result *ResolverResult) bool {
	switch strings.ToLower(result.Identifier) {
	case "", "null", "''", `""`, "``":
		return true
	default:
		return false
	}
}

// readAsNumbers returns the two operands with a JSON reading that is compared
// to a number read as a number, so the comparison has two of the same thing on
// either side of it.
func readAsNumbers(d dialect.Dialect, left, right *ResolverResult) (*ResolverResult, *ResolverResult) {
	if left.Extracted && bindsNumber(right) {
		left = withIdentifier(left, d.Number(left.Identifier))
	} else if right.Extracted && bindsNumber(left) {
		right = withIdentifier(right, d.Number(right.Identifier))
	}

	return left, right
}

// bindsNumber reports whether the operand is a single bound number.
func bindsNumber(result *ResolverResult) bool {
	for key, value := range result.Params {
		if result.Identifier != "{:"+key+"}" {
			continue
		}

		switch value.(type) {
		case float32, float64,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			return true
		}
	}

	return false
}

func withIdentifier(result *ResolverResult, identifier string) *ResolverResult {
	copied := *result
	copied.Identifier = identifier
	return &copied
}

func isLikeOp(op fexpr.SignOp) bool {
	switch op {
	case
		fexpr.SignLike,
		fexpr.SignAnyLike,
		fexpr.SignNlike,
		fexpr.SignAnyNlike:
		return true
	}

	return false
}

func isAnyMatchOp(op fexpr.SignOp) bool {
	switch op {
	case
		fexpr.SignAnyEq,
		fexpr.SignAnyNeq,
		fexpr.SignAnyLike,
		fexpr.SignAnyNlike,
		fexpr.SignAnyLt,
		fexpr.SignAnyLte,
		fexpr.SignAnyGt,
		fexpr.SignAnyGte:
		return true
	}

	return false
}

// mergeParams returns new query.Params where each provided params item
// is merged in the order they are specified.
func mergeParams(params ...query.Params) query.Params {
	result := query.Params{}

	for _, p := range params {
		for k, v := range p {
			result[k] = v
		}
	}

	return result
}

// @todo consider adding support for custom single character wildcard
//
// wrapLikeParams wraps each provided param value string with `%`
// if the param doesn't contain an explicit wildcard (`%`) character already.
func wrapLikeParams(params query.Params) query.Params {
	result := query.Params{}

	for k, v := range params {
		vStr := cast.ToString(v)
		if !containsUnescapedChar(vStr, '%') {
			// note: this is done to minimize the breaking changes and to preserve the original autoescape behavior
			vStr = escapeUnescapedChars(vStr, '\\', '%', '_')
			vStr = "%" + vStr + "%"
		}
		result[k] = vStr
	}

	return result
}

func escapeUnescapedChars(str string, escapeChars ...rune) string {
	rs := []rune(str)
	total := len(rs)
	result := make([]rune, 0, total)

	var match bool

	for i := total - 1; i >= 0; i-- {
		if match {
			// check if already escaped
			if rs[i] != '\\' {
				result = append(result, '\\')
			}
			match = false
		} else {
			for _, ec := range escapeChars {
				if rs[i] == ec {
					match = true
					break
				}
			}
		}

		result = append(result, rs[i])

		// in case the matching char is at the beginning
		if i == 0 && match {
			result = append(result, '\\')
		}
	}

	// reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return string(result)
}

func containsUnescapedChar(str string, ch rune) bool {
	var prev rune

	for _, c := range str {
		if c == ch && prev != '\\' {
			return true
		}

		if c == '\\' && prev == '\\' {
			prev = rune(0) // reset escape sequence
		} else {
			prev = c
		}
	}

	return false
}

// -------------------------------------------------------------------

var _ query.Expression = (*opExpr)(nil)

// opExpr defines an expression that contains a raw sql operator string.
type opExpr struct {
	op string
}

// Build converts the expression into a SQL fragment.
//
// Implements [query.Expression] interface.
func (e *opExpr) Build(db *query.DB, params query.Params) string {
	return e.op
}

// -------------------------------------------------------------------

var _ query.Expression = (*concatExpr)(nil)

// concatExpr defines an expression that concatenates multiple
// other expressions with a specified separator.
type concatExpr struct {
	separator string
	parts     []query.Expression
}

// Build converts the expression into a SQL fragment.
//
// Implements [query.Expression] interface.
func (e *concatExpr) Build(db *query.DB, params query.Params) string {
	if len(e.parts) == 0 {
		return ""
	}

	stringParts := make([]string, 0, len(e.parts))

	for _, p := range e.parts {
		if p == nil {
			continue
		}

		if sql := p.Build(db, params); sql != "" {
			stringParts = append(stringParts, sql)
		}
	}

	// skip extra parenthesis for single concat expression
	if len(stringParts) == 1 &&
		// check for already concatenated raw/plain expressions
		!strings.Contains(strings.ToUpper(stringParts[0]), " AND ") &&
		!strings.Contains(strings.ToUpper(stringParts[0]), " OR ") {
		return stringParts[0]
	}

	return "(" + strings.Join(stringParts, e.separator) + ")"
}

// -------------------------------------------------------------------

// multiMatch is the extra condition a multi-valued operand contributes, or nil
// where neither operand is one.
//
// Comparing to a set means comparing to every member of it, and the join alone
// says only that SOME member matched, so the set is compared again in a
// subquery. Which of the three forms applies is a property of the operands.
func multiMatch(d dialect.Dialect, left *ResolverResult, op fexpr.SignOp, right *ResolverResult) (query.Expression, error) {
	switch {
	case left.MultiMatchSubQuery != nil && right.MultiMatchSubQuery != nil:
		return newManyVsMany(d, left, op, right)
	case left.MultiMatchSubQuery != nil:
		return newManyVsOne(d, left, op, right, false)
	case right.MultiMatchSubQuery != nil:
		return newManyVsOne(d, right, op, left, true)
	}

	return nil, nil
}

// -------------------------------------------------------------------

var _ query.Expression = (*manyVsManyExpr)(nil)

// manyVsManyExpr pairs two multi-valued operands: no pairing may fail the
// comparison, which over two sets is what "every value matches" means.
//
// Each subquery returns a single "multiMatchValue" column.
type manyVsManyExpr struct {
	left       *MultiMatchSubquery
	right      *MultiMatchSubquery
	leftAlias  string
	rightAlias string
	where      query.Expression
}

// newManyVsMany assembles a many<->many multi-match, or says why it cannot.
//
// Everything that can fail happens here, where saying so is an error the caller
// turns into a refused filter. [query.Expression.Build] answers a string and
// nothing else, so a builder that fails there has only a fragment of SQL to say
// it with — and a fragment means whatever the SQL around it makes it mean. "0=1"
// reads as "no rows" ANDed and as "every row" negated, and this expression is
// built out of both.
func newManyVsMany(d dialect.Dialect, left *ResolverResult, op fexpr.SignOp, right *ResolverResult) (query.Expression, error) {
	if err := left.MultiMatchSubQuery.usable(); err != nil {
		return nil, err
	}

	if err := right.MultiMatchSubQuery.usable(); err != nil {
		return nil, err
	}

	e := &manyVsManyExpr{
		left:       left.MultiMatchSubQuery,
		right:      right.MultiMatchSubQuery,
		leftAlias:  "__ml" + security.PseudorandomString(8),
		rightAlias: "__mr" + security.PseudorandomString(8),
	}

	var err error

	e.where, err = buildResolversExpr(
		d,
		&ResolverResult{
			NullFallback: left.NullFallback,
			Extracted:    left.Extracted,
			Identifier:   "[[" + e.leftAlias + ".multiMatchValue]]",
		},
		op,
		&ResolverResult{
			NullFallback: right.NullFallback,
			Extracted:    right.Extracted,
			Identifier:   "[[" + e.rightAlias + ".multiMatchValue]]",
			// note: the AfterBuild needs to be handled only once and it
			// doesn't matter whether it is applied on the left or right subquery operand
			AfterBuild: query.Not, // inverse for the not-exist expression
		},
	)
	if err != nil {
		return nil, err
	}

	return e, nil
}

// Build converts the expression into a SQL fragment.
//
// Implements [query.Expression] interface.
func (e *manyVsManyExpr) Build(db *query.DB, params query.Params) string {
	// the two operand subqueries are paired row by row rather than matched on
	// anything of their own, and an outer join still has to state a condition
	return fmt.Sprintf(
		"NOT EXISTS (SELECT 1 FROM (%s) {{%s}} LEFT JOIN (%s) {{%s}} ON 1=1 WHERE %s)",
		e.left.Build(db, params),
		e.leftAlias,
		e.right.Build(db, params),
		e.rightAlias,
		e.where.Build(db, params),
	)
}

// -------------------------------------------------------------------

var _ query.Expression = (*manyVsOneExpr)(nil)

// manyVsOneExpr compares every value of a multi-valued operand against a single
// one: no value may fail the comparison.
//
// The subquery returns a single "multiMatchValue" column.
type manyVsOneExpr struct {
	subQuery *MultiMatchSubquery
	alias    string
	where    query.Expression
}

// newManyVsOne assembles a many<->one multi-match, or says why it cannot.
// Pass inverse to put the single-valued operand on the left.
//
// See [newManyVsMany] for why the failures are here rather than in Build.
func newManyVsOne(d dialect.Dialect, many *ResolverResult, op fexpr.SignOp, one *ResolverResult, inverse bool) (query.Expression, error) {
	if err := many.MultiMatchSubQuery.usable(); err != nil {
		return nil, err
	}

	e := &manyVsOneExpr{
		subQuery: many.MultiMatchSubQuery,
		alias:    "__sm" + security.PseudorandomString(8),
	}

	manyOperand := &ResolverResult{
		NullFallback: many.NullFallback,
		Extracted:    many.Extracted,
		Identifier:   "[[" + e.alias + ".multiMatchValue]]",
		AfterBuild:   query.Not, // inverse for the not-exist expression
	}

	oneOperand := &ResolverResult{
		Identifier: one.Identifier,
		Params:     one.Params,
	}

	var err error

	if inverse {
		e.where, err = buildResolversExpr(d, oneOperand, op, manyOperand)
	} else {
		e.where, err = buildResolversExpr(d, manyOperand, op, oneOperand)
	}
	if err != nil {
		return nil, err
	}

	return e, nil
}

// Build converts the expression into a SQL fragment.
//
// Implements [query.Expression] interface.
func (e *manyVsOneExpr) Build(db *query.DB, params query.Params) string {
	return fmt.Sprintf(
		"NOT EXISTS (SELECT 1 FROM (%s) {{%s}} WHERE %s)",
		e.subQuery.Build(db, params),
		e.alias,
		e.where.Build(db, params),
	)
}
