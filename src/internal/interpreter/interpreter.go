package interpreter

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ─── Types ────────────────────────────────────────────────────────────────────

type Value interface{}

type PseudoError struct {
	Message string
	Line    int
}

func (e *PseudoError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("Line %d: %s", e.Line, e.Message)
	}
	return e.Message
}

type StopSignal struct{}

func (s *StopSignal) Error() string { return "stop" }

type ReturnSignal struct {
	Value Value
}

// ─── Interpreter ──────────────────────────────────────────────────────────────

type Interpreter struct {
	Variables     map[string]Value
	Functions     map[string]*Function
	CurrentList   []Value
	LastCondition bool
}

type Function struct {
	Params []string
	Body   []string
}

func New() *Interpreter {
	return &Interpreter{
		Variables: make(map[string]Value),
		Functions: make(map[string]*Function),
	}
}

// ─── Value helpers ────────────────────────────────────────────────────────────

func parseValue(s string) Value {
	s = strings.TrimSpace(s)

	// quoted string
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return s[1 : len(s)-1]
	}

	// list
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []Value{}
		}
		parts := splitRespectingQuotes(inner, ',')
		var result []Value
		for _, p := range parts {
			result = append(result, coerce(strings.TrimSpace(p)))
		}
		return result
	}

	// json object
	if strings.HasPrefix(s, "{") {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			result := make(map[string]Value)
			for k, v := range obj {
				result[k] = v
			}
			return result
		}
	}

	return coerce(s)
}

func coerce(s string) Value {
	s = strings.TrimSpace(s)
	sl := strings.ToLower(s)
	if sl == "true" {
		return true
	}
	if sl == "false" {
		return false
	}
	if sl == "null" || sl == "nothing" || sl == "none" || sl == "empty" {
		return nil
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func splitRespectingQuotes(s string, sep rune) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune

	for _, ch := range s {
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			}
			current.WriteRune(ch)
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
			current.WriteRune(ch)
		} else if ch == sep {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func (interp *Interpreter) resolve(token string) Value {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'")
	if val, ok := interp.Variables[token]; ok {
		return val
	}
	return coerce(token)
}

func (interp *Interpreter) resolveExpr(expr string) Value {
	expr = strings.TrimSpace(expr)
	if (strings.HasPrefix(expr, "\"") && strings.HasSuffix(expr, "\"")) ||
		(strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'")) {
		return expr[1 : len(expr)-1]
	}
	// dot notation
	if strings.Contains(expr, ".") {
		parts := strings.SplitN(expr, ".", 2)
		obj := interp.resolve(parts[0])
		if m, ok := obj.(map[string]Value); ok {
			return m[parts[1]]
		}
	}
	return interp.resolve(expr)
}

func toFloat(v Value) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	}
	return 0, false
}

func toString(v Value) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case float64:
		if val == math.Trunc(val) {
			return strconv.Itoa(int(val))
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case []Value:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = toString(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]Value:
		parts := make([]string, 0)
		for k, v := range val {
			parts = append(parts, k+": "+toString(v))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprintf("%v", v)
}

func compareValues(a, b Value, op string) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch op {
		case "=", "==":
			return af == bf
		case "!=":
			return af != bf
		case ">":
			return af > bf
		case "<":
			return af < bf
		case ">=":
			return af >= bf
		case "<=":
			return af <= bf
		}
	}
	as, bs := toString(a), toString(b)
	switch op {
	case "=", "==":
		return as == bs
	case "!=":
		return as != bs
	case ">":
		return as > bs
	case "<":
		return as < bs
	}
	return false
}

// ─── Condition evaluation ─────────────────────────────────────────────────────

func (interp *Interpreter) evalCondition(s string) bool {
	s = strings.TrimSpace(s)

	// and
	if idx := indexOfWord(s, " and "); idx >= 0 {
		left := interp.evalCondition(s[:idx])
		right := interp.evalCondition(s[idx+5:])
		return left && right
	}
	// or
	if idx := indexOfWord(s, " or "); idx >= 0 {
		left := interp.evalCondition(s[:idx])
		right := interp.evalCondition(s[idx+4:])
		return left || right
	}
	// not
	if strings.HasPrefix(s, "not ") {
		return !interp.evalCondition(s[4:])
	}

	// X exists in Y
	re := regexp.MustCompile(`"?(.+?)"?\s+exists\s+in\s+(\w+)`)
	if m := re.FindStringSubmatch(s); m != nil {
		needle := m[1]
		haystack := toString(interp.resolve(m[2]))
		return strings.Contains(haystack, needle)
	}

	// X is empty / not empty
	reEmpty := regexp.MustCompile(`(\w+)\s+is\s+not\s+empty`)
	if m := reEmpty.FindStringSubmatch(s); m != nil {
		val := interp.resolve(m[1])
		if val == nil {
			return false
		}
		if lst, ok := val.([]Value); ok {
			return len(lst) > 0
		}
		return toString(val) != ""
	}
	reEmpty2 := regexp.MustCompile(`(\w+)\s+is\s+empty`)
	if m := reEmpty2.FindStringSubmatch(s); m != nil {
		val := interp.resolve(m[1])
		if val == nil {
			return true
		}
		if lst, ok := val.([]Value); ok {
			return len(lst) == 0
		}
		return toString(val) == ""
	}

	// standard comparison
	ops := []string{">=", "<=", "!=", ">", "<", "==", "="}
	for _, op := range ops {
		idx := strings.Index(s, op)
		if idx < 0 {
			continue
		}
		left := strings.TrimSpace(s[:idx])
		right := strings.TrimSpace(s[idx+len(op):])
		right = strings.Trim(right, "\"'")
		leftVal := interp.resolveExpr(left)
		rightVal := interp.resolve(right)
		return compareValues(leftVal, rightVal, op)
	}

	return false
}

func indexOfWord(s, word string) int {
	idx := strings.Index(s, word)
	return idx
}

func (interp *Interpreter) filterList(lst []Value, condition string, keep bool) []Value {
	var result []Value
	re := regexp.MustCompile(`(\w+)\s*(>=|<=|!=|>|<|==|=)\s*(.+)`)
	m := re.FindStringSubmatch(strings.TrimSpace(condition))
	if m == nil {
		return lst
	}
	prop := m[1]
	op := m[2]
	valStr := strings.Trim(strings.TrimSpace(m[3]), "\"'")
	rightVal := interp.resolve(valStr)

	for _, item := range lst {
		var itemVal Value
		if obj, ok := item.(map[string]Value); ok {
			itemVal = obj[prop]
		} else {
			itemVal = item
		}
		matches := compareValues(itemVal, rightVal, op)
		if (keep && matches) || (!keep && !matches) {
			result = append(result, item)
		}
	}
	return result
}

// ─── Execute ──────────────────────────────────────────────────────────────────

func (interp *Interpreter) Execute(code string) {
	lines := strings.Split(code, "\n")
	interp.executeLines(lines, 0, len(lines))
}

func (interp *Interpreter) executeLines(lines []string, start, end int) {
	i := start
	for i < end {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			i++
			continue
		}
		// try new features first
		jumpV2, handled, errV2 := interp.execLineV2(line, lines, i, end)
		if handled {
			if errV2 != nil {
				if _, ok := errV2.(*StopSignal); ok {
					return
				}
				fmt.Printf("\n  [Pseudo Error] %s\n\n", errV2.Error())
				i++
				continue
			}
			if jumpV2 >= 0 {
				i = jumpV2
			} else {
				i++
			}
			continue
		}
		next, err := interp.executeLine(line, lines, i, end)
		if err != nil {
			if _, ok := err.(*StopSignal); ok {
				return
			}
			fmt.Printf("\n  [Pseudo Error] %s\n\n", err.Error())
			i++
			continue
		}
		if next >= 0 {
			i = next
		} else {
			i++
		}
	}
}

func (interp *Interpreter) collectBlock(lines []string, start, end int) ([]string, int) {
	var body []string
	depth := 0
	for j := start + 1; j < end; j++ {
		stripped := strings.TrimSpace(lines[j])
		if strings.HasPrefix(stripped, "define ") ||
			strings.HasPrefix(stripped, "repeat ") ||
			strings.HasPrefix(stripped, "for each ") {
			depth++
		}
		if stripped == "done" {
			if depth == 0 {
				return body, j
			}
			depth--
		}
		body = append(body, lines[j])
	}
	return body, end
}

func (interp *Interpreter) executeLine(line string, lines []string, idx, end int) (int, error) {

	// ── variable assignment ───────────────────────────────────────────────────
	if isAssignment(line) {
		eqIdx := strings.Index(line, "=")
		varName := strings.TrimSpace(line[:eqIdx])
		valStr := strings.TrimSpace(line[eqIdx+1:])
		interp.Variables[varName] = parseValue(valStr)
		if lst, ok := interp.Variables[varName].([]Value); ok {
			interp.CurrentList = lst
		}
		return -1, nil
	}

	// ── define function ───────────────────────────────────────────────────────
	if re := matchLine(`^define\s+(\w+)(?:\s+with\s+(.+))?$`, line); re != nil {
		funcName := re[1]
		var params []string
		if re[2] != "" {
			for _, p := range strings.Split(re[2], ",") {
				params = append(params, strings.TrimSpace(p))
			}
		}
		body, endIdx := interp.collectBlock(lines, idx, end)
		interp.Functions[funcName] = &Function{Params: params, Body: body}
		return endIdx + 1, nil
	}

	// ── run function ──────────────────────────────────────────────────────────
	if re := matchLine(`^run\s+(\w+)(?:\s+with\s+(.+))?$`, line); re != nil {
		funcName := re[1]
		fn, ok := interp.Functions[funcName]
		if !ok {
			return -1, &PseudoError{Message: fmt.Sprintf("Function '%s' is not defined. Did you forget to define it?", funcName)}
		}
		var args []Value
		if re[2] != "" {
			for _, a := range splitRespectingQuotes(re[2], ',') {
				args = append(args, interp.resolveExpr(strings.TrimSpace(a)))
			}
		}
		sub := New()
		for k, v := range interp.Variables {
			sub.Variables[k] = v
		}
		for k, v := range interp.Functions {
			sub.Functions[k] = v
		}
		sub.CurrentList = interp.CurrentList
		for i, p := range fn.Params {
			if i < len(args) {
				sub.Variables[p] = args[i]
			}
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					if ret, ok := r.(ReturnSignal); ok {
						interp.Variables["result"] = ret.Value
					}
				}
			}()
			sub.executeLines(fn.Body, 0, len(fn.Body))
		}()
		for k, v := range sub.Variables {
			if !contains(fn.Params, k) {
				interp.Variables[k] = v
			}
		}
		return -1, nil
	}

	// ── return ────────────────────────────────────────────────────────────────
	if re := matchLine(`^return\s+(.+)$`, line); re != nil {
		panic(ReturnSignal{Value: interp.resolveExpr(re[1])})
	}

	// ── repeat N times ────────────────────────────────────────────────────────
	if re := matchLine(`^repeat\s+(.+?)\s+times$`, line); re != nil {
		nVal := interp.resolve(re[1])
		nf, _ := toFloat(nVal)
		n := int(nf)
		body, endIdx := interp.collectBlock(lines, idx, end)
		for iter := 0; iter < n; iter++ {
			sub := interp.subInterp()
			sub.Variables["iteration"] = iter + 1
			sub.executeLines(body, 0, len(body))
			interp.mergeFrom(sub)
		}
		return endIdx + 1, nil
	}

	// ── for each item in list ─────────────────────────────────────────────────
	if re := matchLine(`^for\s+each\s+(\w+)\s+in\s+(\w+)$`, line); re != nil {
		itemName := re[1]
		listName := re[2]
		target := interp.resolve(listName)
		var lst []Value
		if l, ok := target.([]Value); ok {
			lst = l
		} else {
			lst = []Value{target}
		}
		body, endIdx := interp.collectBlock(lines, idx, end)
		for _, item := range lst {
			sub := interp.subInterp()
			sub.Variables[itemName] = item
			sub.executeLines(body, 0, len(body))
			for k, v := range sub.Variables {
				if k != itemName {
					interp.Variables[k] = v
				}
			}
		}
		return endIdx + 1, nil
	}

	// ── go in list ────────────────────────────────────────────────────────────
	if re := matchLine(`^go\s+in\s+(.+?)(?:\s+and\s+check\s+if\s+"?(.+?)"?\s+exists)?$`, line); re != nil {
		listName := strings.TrimSpace(re[1])
		searchTerm := re[2]
		target := interp.resolve(listName)
		if searchTerm != "" {
			interp.LastCondition = strings.Contains(toString(target), searchTerm)
		} else {
			if lst, ok := target.([]Value); ok {
				interp.CurrentList = lst
			} else {
				words := strings.Fields(toString(target))
				var lst []Value
				for _, w := range words {
					lst = append(lst, w)
				}
				interp.CurrentList = lst
			}
		}
		return -1, nil
	}

	// ── find each where ───────────────────────────────────────────────────────
	if re := matchLine(`^find\s+each\s+\w+\s+where\s+(.+)$`, line); re != nil {
		interp.CurrentList = interp.filterList(interp.CurrentList, re[1], true)
		return -1, nil
	}

	// ── remove from list where ────────────────────────────────────────────────
	if re := matchLine(`^remove\s+from\s+(\w+)\s+where\s+(.+)$`, line); re != nil {
		listName := re[1]
		target := interp.resolve(listName)
		if lst, ok := target.([]Value); ok {
			result := interp.filterList(lst, re[2], false)
			interp.Variables[listName] = result
			interp.CurrentList = result
		}
		return -1, nil
	}

	// ── check each item if X exists ───────────────────────────────────────────
	if re := matchLine(`^check\s+each\s+\w+\s+if\s+"?(.+?)"?\s+exists$`, line); re != nil {
		searchTerm := re[1]
		var valid, invalid []string
		for _, item := range interp.CurrentList {
			if strings.Contains(toString(item), searchTerm) {
				valid = append(valid, toString(item))
			} else {
				invalid = append(invalid, toString(item))
			}
		}
		fmt.Printf("  valid:   [%s]\n", strings.Join(valid, ", "))
		fmt.Printf("  invalid: [%s]\n", strings.Join(invalid, ", "))
		return -1, nil
	}

	// ── check if condition ────────────────────────────────────────────────────
	if re := matchLine(`^check\s+if\s+(.+)$`, line); re != nil {
		interp.LastCondition = interp.evalCondition(re[1])
		return -1, nil
	}

	// ── if X → outcome ────────────────────────────────────────────────────────
	if re := matchLine(`^if\s+(.+?)\s*→\s*(.+)$`, line); re != nil {
		cond := strings.TrimSpace(re[1])
		outcome := strings.TrimSpace(re[2])
		var result bool
		if cond == "yes" {
			result = interp.LastCondition
		} else if cond == "no" {
			result = !interp.LastCondition
		} else {
			result = interp.evalCondition(cond)
			interp.LastCondition = result
		}
		if result {
			interp.doOutcome(outcome)
		}
		return -1, nil
	}

	// ── else → outcome ────────────────────────────────────────────────────────
	if re := matchLine(`^else\s*→\s*(.+)$`, line); re != nil {
		if !interp.LastCondition {
			interp.doOutcome(strings.TrimSpace(re[1]))
		}
		return -1, nil
	}

	// ── STRING OPERATIONS ─────────────────────────────────────────────────────

	if re := matchLine(`^make\s+(\w+)\s+(uppercase|lowercase|trimmed)$`, line); re != nil {
		varName, op := re[1], re[2]
		val := toString(interp.resolve(varName))
		switch op {
		case "uppercase":
			interp.Variables[varName] = strings.ToUpper(val)
		case "lowercase":
			interp.Variables[varName] = strings.ToLower(val)
		case "trimmed":
			interp.Variables[varName] = strings.TrimSpace(val)
		}
		return -1, nil
	}

	if re := matchLine(`^replace\s+"(.+?)"\s+with\s+"(.+?)"\s+in\s+(\w+)$`, line); re != nil {
		old, new_, varName := re[1], re[2], re[3]
		val := toString(interp.resolve(varName))
		interp.Variables[varName] = strings.ReplaceAll(val, old, new_)
		return -1, nil
	}

	if re := matchLine(`^split\s+(\w+)\s+by\s+"(.+?)"\s+into\s+(\w+)$`, line); re != nil {
		varName, sep, resultName := re[1], re[2], re[3]
		val := toString(interp.resolve(varName))
		parts := strings.Split(val, sep)
		var lst []Value
		for _, p := range parts {
			lst = append(lst, p)
		}
		interp.Variables[resultName] = lst
		return -1, nil
	}

	if re := matchLine(`^join\s+(\w+)\s+with\s+"(.+?)"\s+into\s+(\w+)$`, line); re != nil {
		listName, sep, resultName := re[1], re[2], re[3]
		target := interp.resolve(listName)
		if lst, ok := target.([]Value); ok {
			parts := make([]string, len(lst))
			for i, item := range lst {
				parts[i] = toString(item)
			}
			interp.Variables[resultName] = strings.Join(parts, sep)
		}
		return -1, nil
	}

	if re := matchLine(`^get\s+(first|last)\s+(\d+)\s+characters?\s+of\s+(\w+)\s+into\s+(\w+)$`, line); re != nil {
		direction, nStr, varName, resultName := re[1], re[2], re[3], re[4]
		n, _ := strconv.Atoi(nStr)
		val := toString(interp.resolve(varName))
		if direction == "first" {
			if n > len(val) {
				n = len(val)
			}
			interp.Variables[resultName] = val[:n]
		} else {
			if n > len(val) {
				n = len(val)
			}
			interp.Variables[resultName] = val[len(val)-n:]
		}
		return -1, nil
	}

	if re := matchLine(`^convert\s+(\w+)\s+to\s+(number|string|integer)(?:\s+into\s+(\w+))?$`, line); re != nil {
		varName, toType, resultName := re[1], re[2], re[3]
		val := interp.resolve(varName)
		target := varName
		if resultName != "" {
			target = resultName
		}
		switch toType {
		case "number", "integer":
			if f, ok := toFloat(val); ok {
				interp.Variables[target] = int(f)
			}
		case "string":
			interp.Variables[target] = toString(val)
		}
		return -1, nil
	}

	// ── MATH ──────────────────────────────────────────────────────────────────

	if re := matchLine(`^calculate\s+(.+?)\s+into\s+(\w+)$`, line); re != nil {
		expr, varName := re[1], re[2]
		result := interp.evalMath(expr)
		interp.Variables[varName] = result
		return -1, nil
	}

	if re := matchLine(`^multiply\s+(\w+)\s+by\s+(.+)$`, line); re != nil {
		varName := re[1]
		amount, _ := toFloat(interp.resolve(re[2]))
		current, _ := toFloat(interp.resolve(varName))
		result := current * amount
		if result == math.Trunc(result) {
			interp.Variables[varName] = int(result)
		} else {
			interp.Variables[varName] = result
		}
		return -1, nil
	}

	if re := matchLine(`^subtract\s+(.+?)\s+from\s+(\w+)$`, line); re != nil {
		amount, _ := toFloat(interp.resolve(re[1]))
		varName := re[2]
		current, _ := toFloat(interp.resolve(varName))
		result := current - amount
		if result == math.Trunc(result) {
			interp.Variables[varName] = int(result)
		} else {
			interp.Variables[varName] = result
		}
		return -1, nil
	}

	if re := matchLine(`^increase\s+(\w+)\s+by\s+(.+)$`, line); re != nil {
		varName := re[1]
		amount, _ := toFloat(interp.resolve(re[2]))
		current, _ := toFloat(interp.resolve(varName))
		result := current + amount
		if result == math.Trunc(result) {
			interp.Variables[varName] = int(result)
		} else {
			interp.Variables[varName] = result
		}
		return -1, nil
	}

	if re := matchLine(`^decrease\s+(\w+)\s+by\s+(.+)$`, line); re != nil {
		varName := re[1]
		amount, _ := toFloat(interp.resolve(re[2]))
		current, _ := toFloat(interp.resolve(varName))
		result := current - amount
		if result == math.Trunc(result) {
			interp.Variables[varName] = int(result)
		} else {
			interp.Variables[varName] = result
		}
		return -1, nil
	}

	if re := matchLine(`^divide\s+(\w+)\s+by\s+(.+)$`, line); re != nil {
		val, _ := toFloat(interp.resolve(re[1]))
		divisor, _ := toFloat(interp.resolve(re[2]))
		if divisor == 0 {
			return -1, &PseudoError{Message: "Cannot divide by zero."}
		}
		interp.Variables["remainder"] = int(math.Mod(val, divisor))
		interp.Variables["quotient"] = int(val / divisor)
		return -1, nil
	}

	if re := matchLine(`^add\s+all\s+(\w+)?\s*together$`, line); re != nil {
		varName := strings.TrimSpace(re[1])
		var target []Value
		if varName != "" {
			if lst, ok := interp.resolve(varName).([]Value); ok {
				target = lst
			}
		} else {
			target = interp.CurrentList
		}
		total := 0.0
		for _, item := range target {
			if f, ok := toFloat(item); ok {
				total += f
			}
		}
		if total == math.Trunc(total) {
			interp.Variables["total"] = int(total)
		} else {
			interp.Variables["total"] = total
		}
		return -1, nil
	}

	if re := matchLine(`^compare\s+(\w+)?(?:\s+with\s+each\s+other)?$`, line); re != nil {
		varName := strings.TrimSpace(re[1])
		var target []Value
		if varName != "" {
			if lst, ok := interp.resolve(varName).([]Value); ok {
				target = lst
			}
		} else {
			target = interp.CurrentList
		}
		if len(target) > 0 {
			maxVal, _ := toFloat(target[0])
			minVal := maxVal
			for _, item := range target[1:] {
				if f, ok := toFloat(item); ok {
					if f > maxVal {
						maxVal = f
					}
					if f < minVal {
						minVal = f
					}
				}
			}
			if maxVal == math.Trunc(maxVal) {
				interp.Variables["biggest"] = int(maxVal)
			} else {
				interp.Variables["biggest"] = maxVal
			}
			if minVal == math.Trunc(minVal) {
				interp.Variables["smallest"] = int(minVal)
			} else {
				interp.Variables["smallest"] = minVal
			}
		}
		return -1, nil
	}

	// ── COUNT ─────────────────────────────────────────────────────────────────

	if re := matchLine(`^count\s+characters\s+in\s+(\w+)$`, line); re != nil {
		val := toString(interp.resolve(re[1]))
		interp.Variables["count"] = len(val)
		return -1, nil
	}

	if re := matchLine(`^count\s+(?:words\s+in\s+)?(?:list\s+)?(\w+)?$`, line); re != nil {
		varName := strings.TrimSpace(re[1])
		var count int
		if varName != "" {
			val := interp.resolve(varName)
			if lst, ok := val.([]Value); ok {
				count = len(lst)
			} else {
				count = len(strings.Fields(toString(val)))
			}
		} else if interp.CurrentList != nil {
			count = len(interp.CurrentList)
		}
		interp.Variables["count"] = count
		fmt.Printf("  count: %d\n", count)
		return -1, nil
	}

	// ── RANGE ─────────────────────────────────────────────────────────────────

	if re := matchLine(`^start\s+from\s+(.+)$`, line); re != nil {
		val := interp.resolve(re[1])
		f, _ := toFloat(val)
		interp.Variables["_range_start"] = int(f)
		return -1, nil
	}

	if re := matchLine(`^go\s+up\s+to\s+(.+)$`, line); re != nil {
		val := interp.resolve(re[1])
		f, _ := toFloat(val)
		end_ := int(f)
		start := 0
		if s, ok := interp.Variables["_range_start"].(int); ok {
			start = s
		}
		var lst []Value
		for i := start; i <= end_; i++ {
			lst = append(lst, i)
		}
		interp.CurrentList = lst
		return -1, nil
	}

	// ── DATE & TIME ───────────────────────────────────────────────────────────

	if re := matchLine(`^get\s+today\s+into\s+(\w+)$`, line); re != nil {
		interp.Variables[re[1]] = time.Now().Format("2006-01-02")
		return -1, nil
	}

	if re := matchLine(`^get\s+now\s+into\s+(\w+)$`, line); re != nil {
		interp.Variables[re[1]] = time.Now().Format("2006-01-02 15:04:05")
		return -1, nil
	}

	if re := matchLine(`^get\s+(year|month|day|hour|minute|second)\s+into\s+(\w+)$`, line); re != nil {
		now := time.Now()
		part := re[1]
		varName := re[2]
		switch part {
		case "year":
			interp.Variables[varName] = now.Year()
		case "month":
			interp.Variables[varName] = int(now.Month())
		case "day":
			interp.Variables[varName] = now.Day()
		case "hour":
			interp.Variables[varName] = now.Hour()
		case "minute":
			interp.Variables[varName] = now.Minute()
		case "second":
			interp.Variables[varName] = now.Second()
		}
		return -1, nil
	}

	// ── HTTP ──────────────────────────────────────────────────────────────────

	if re := matchLine(`^(?:fetch|call\s+api)\s+"(.+?)"\s+into\s+(\w+)$`, line); re != nil {
		url_, varName := re[1], re[2]
		resp, err := http.Get(url_)
		if err != nil {
			return -1, &PseudoError{Message: fmt.Sprintf("Couldn't fetch '%s': %v", url_, err)}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var parsed interface{}
		if json.Unmarshal(body, &parsed) == nil {
			interp.Variables[varName] = parsed
		} else {
			interp.Variables[varName] = string(body)
		}
		fmt.Printf("  fetched %s\n", url_)
		return -1, nil
	}

	// ── OBJECTS ───────────────────────────────────────────────────────────────

	if re := matchLine(`^make\s+object\s+(\w+)\s+with\s+(.+)$`, line); re != nil {
		objName := re[1]
		obj := make(map[string]Value)
		for _, pair := range splitRespectingQuotes(re[2], ',') {
			pair = strings.TrimSpace(pair)
			if idx := strings.Index(pair, ":"); idx >= 0 {
				k := strings.TrimSpace(pair[:idx])
				v := parseValue(strings.TrimSpace(pair[idx+1:]))
				obj[k] = v
			}
		}
		interp.Variables[objName] = obj
		return -1, nil
	}

	if re := matchLine(`^set\s+(\w+)\.(\w+)\s+to\s+(.+)$`, line); re != nil {
		objName, key, valStr := re[1], re[2], re[3]
		obj, ok := interp.Variables[objName].(map[string]Value)
		if !ok {
			obj = make(map[string]Value)
		}
		obj[key] = parseValue(valStr)
		interp.Variables[objName] = obj
		return -1, nil
	}

	if re := matchLine(`^get\s+(\w+)\.(\w+)\s+into\s+(\w+)$`, line); re != nil {
		objName, key, varName := re[1], re[2], re[3]
		if obj, ok := interp.Variables[objName].(map[string]Value); ok {
			interp.Variables[varName] = obj[key]
		}
		return -1, nil
	}

	// ── LIST OPERATIONS ───────────────────────────────────────────────────────

	if re := matchLine(`^append\s+(.+?)\s+to\s+(\w+)$`, line); re != nil {
		val := interp.resolveExpr(strings.TrimSpace(re[1]))
		listName := re[2]
		lst, _ := interp.Variables[listName].([]Value)
		interp.Variables[listName] = append(lst, val)
		return -1, nil
	}

	if re := matchLine(`^sort\s+(\w+)\s+(ascending|descending)$`, line); re != nil {
		listName, direction := re[1], re[2]
		if lst, ok := interp.resolve(listName).([]Value); ok {
			sorted := make([]Value, len(lst))
			copy(sorted, lst)
			sort.Slice(sorted, func(i, j int) bool {
				fi, iok := toFloat(sorted[i])
				fj, jok := toFloat(sorted[j])
				if iok && jok {
					if direction == "ascending" {
						return fi < fj
					}
					return fi > fj
				}
				if direction == "ascending" {
					return toString(sorted[i]) < toString(sorted[j])
				}
				return toString(sorted[i]) > toString(sorted[j])
			})
			interp.Variables[listName] = sorted
			interp.CurrentList = sorted
		}
		return -1, nil
	}

	if re := matchLine(`^reverse\s+(\w+)$`, line); re != nil {
		listName := re[1]
		if lst, ok := interp.resolve(listName).([]Value); ok {
			reversed := make([]Value, len(lst))
			for i, item := range lst {
				reversed[len(lst)-1-i] = item
			}
			interp.Variables[listName] = reversed
			interp.CurrentList = reversed
		}
		return -1, nil
	}

	// ── INPUT / OUTPUT ────────────────────────────────────────────────────────

	if re := matchLine(`^ask\s+(\w+)\s+"(.+)"$`, line); re != nil {
		varName, question := re[1], re[2]
		fmt.Printf("  %s ", question)
		var input string
		fmt.Scanln(&input)
		interp.Variables[varName] = input
		return -1, nil
	}

	if re := matchLine(`^save\s+(\w+)\s+to\s+"(.+)"$`, line); re != nil {
		varName, filename := re[1], re[2]
		val := interp.resolve(varName)
		var content string
		if lst, ok := val.([]Value); ok {
			parts := make([]string, len(lst))
			for i, item := range lst {
				parts[i] = toString(item)
			}
			content = strings.Join(parts, "\n")
		} else if m, ok := val.(map[string]Value); ok {
			data := make(map[string]interface{})
			for k, v := range m {
				data[k] = v
			}
			b, _ := json.MarshalIndent(data, "", "  ")
			content = string(b)
		} else {
			content = toString(val)
		}
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			return -1, &PseudoError{Message: fmt.Sprintf("Couldn't save to '%s': %v", filename, err)}
		}
		fmt.Printf("  saved to %s\n", filename)
		return -1, nil
	}

	if re := matchLine(`^load\s+(\w+)\s+from\s+"(.+)"$`, line); re != nil {
		varName, filename := re[1], re[2]
		data, err := os.ReadFile(filename)
		if err != nil {
			return -1, &PseudoError{Message: fmt.Sprintf("File '%s' not found.", filename)}
		}
		var parsed interface{}
		if json.Unmarshal(data, &parsed) == nil {
			interp.Variables[varName] = parsed
		} else {
			interp.Variables[varName] = string(data)
		}
		fmt.Printf("  loaded %s\n", filename)
		return -1, nil
	}

	if re := matchLine(`^wait\s+(.+?)\s+seconds?$`, line); re != nil {
		f, _ := toFloat(interp.resolve(re[1]))
		time.Sleep(time.Duration(f * float64(time.Second)))
		return -1, nil
	}

	// ── PRINT / SAY ───────────────────────────────────────────────────────────

	if line == "print each" || line == "print all" || line == "say each" || line == "say all" {
		for _, item := range interp.CurrentList {
			fmt.Printf("  %s\n", toString(item))
		}
		return -1, nil
	}

	if strings.HasPrefix(line, "print ") || strings.HasPrefix(line, "say ") {
		rest := strings.TrimPrefix(strings.TrimPrefix(line, "print "), "say ")
		rest = strings.TrimSpace(rest)
		interp.doPrint(rest)
		return -1, nil
	}

	// ── if count/remainder inline ─────────────────────────────────────────────

	if re := matchLine(`^if\s+count\s*(>=|<=|>|<|=|!=)\s*(\S+)\s*→\s*(.+)$`, line); re != nil {
		op, valStr, outcome := re[1], re[2], re[3]
		count, _ := toFloat(interp.Variables["count"])
		val, _ := toFloat(interp.resolve(valStr))
		result := compareValues(count, val, op)
		interp.LastCondition = result
		if result {
			interp.doOutcome(strings.TrimSpace(outcome))
		}
		return -1, nil
	}

	if re := matchLine(`^if\s+remainder\s*(>=|<=|>|<|=|!=)\s*(\S+)\s*→\s*(.+)$`, line); re != nil {
		op, valStr, outcome := re[1], re[2], re[3]
		remainder, _ := toFloat(interp.Variables["remainder"])
		val, _ := toFloat(interp.resolve(valStr))
		if op == "=" {
			op = "=="
		}
		result := compareValues(remainder, val, op)
		interp.LastCondition = result
		if result {
			interp.doOutcome(strings.TrimSpace(outcome))
		}
		return -1, nil
	}

	// ── stop ──────────────────────────────────────────────────────────────────
	if line == "stop" {
		return -1, &StopSignal{}
	}

	return -1, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (interp *Interpreter) subInterp() *Interpreter {
	sub := New()
	for k, v := range interp.Variables {
		sub.Variables[k] = v
	}
	for k, v := range interp.Functions {
		sub.Functions[k] = v
	}
	sub.CurrentList = interp.CurrentList
	sub.LastCondition = interp.LastCondition
	return sub
}

func (interp *Interpreter) mergeFrom(sub *Interpreter) {
	for k, v := range sub.Variables {
		interp.Variables[k] = v
	}
	interp.CurrentList = sub.CurrentList
	interp.LastCondition = sub.LastCondition
}

func (interp *Interpreter) doPrint(rest string) {
	if (strings.HasPrefix(rest, "\"") && strings.HasSuffix(rest, "\"")) ||
		(strings.HasPrefix(rest, "'") && strings.HasSuffix(rest, "'")) {
		fmt.Printf("  %s\n", rest[1:len(rest)-1])
		return
	}
	specials := map[string]string{
		"total": "total", "count": "count", "result": "result",
		"biggest": "biggest", "smallest": "smallest",
		"remainder": "remainder", "quotient": "quotient",
		"the biggest": "biggest", "the smallest": "smallest",
		"the bigger one": "biggest",
	}
	if key, ok := specials[rest]; ok {
		fmt.Printf("  %s\n", toString(interp.Variables[key]))
		return
	}
	if strings.Contains(rest, ".") {
		fmt.Printf("  %s\n", toString(interp.resolveExpr(rest)))
		return
	}
	val := interp.resolve(rest)
	if lst, ok := val.([]Value); ok {
		for _, item := range lst {
			fmt.Printf("  %s\n", toString(item))
		}
	} else if m, ok := val.(map[string]Value); ok {
		for k, v := range m {
			fmt.Printf("  %s: %s\n", k, toString(v))
		}
	} else {
		fmt.Printf("  %s\n", toString(val))
	}
}

func (interp *Interpreter) doOutcome(outcome string) {
	if strings.HasPrefix(outcome, "say ") || strings.HasPrefix(outcome, "print ") {
		msg := strings.TrimPrefix(strings.TrimPrefix(outcome, "say "), "print ")
		msg = strings.TrimSpace(msg)
		msg = strings.Trim(msg, "\"'")
		if val, ok := interp.Variables[msg]; ok {
			fmt.Printf("  %s\n", toString(val))
		} else {
			fmt.Printf("  %s\n", msg)
		}
	} else if strings.HasPrefix(outcome, "return ") {
		val := interp.resolveExpr(strings.TrimPrefix(outcome, "return "))
		panic(ReturnSignal{Value: val})
	} else {
		re := regexp.MustCompile(`^(\d+)\s+"?(.+)"?$`)
		if m := re.FindStringSubmatch(outcome); m != nil {
			fmt.Printf("  [%s] %s\n", m[1], m[2])
		} else {
			fmt.Printf("  %s\n", outcome)
		}
	}
}

func (interp *Interpreter) evalMath(expr string) Value {
	// replace variable names with values
	for name, val := range interp.Variables {
		if f, ok := toFloat(val); ok {
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			expr = re.ReplaceAllString(expr, fmt.Sprintf("%g", f))
		}
	}
	// replace sqrt(x) with actual sqrt
	reSqrt := regexp.MustCompile(`sqrt\(([^)]+)\)`)
	expr = reSqrt.ReplaceAllStringFunc(expr, func(m string) string {
		inner := reSqrt.FindStringSubmatch(m)[1]
		if f, err := strconv.ParseFloat(inner, 64); err == nil {
			return fmt.Sprintf("%g", math.Sqrt(f))
		}
		return m
	})
	// simple eval: handle +, -, *, /
	result := evalSimpleMath(expr)
	if result == math.Trunc(result) {
		return int(result)
	}
	return result
}

func evalSimpleMath(expr string) float64 {
	expr = strings.TrimSpace(expr)
	// try direct parse
	if f, err := strconv.ParseFloat(expr, 64); err == nil {
		return f
	}
	// find + or - (lowest precedence, right to left)
	for i := len(expr) - 1; i >= 0; i-- {
		if (expr[i] == '+' || expr[i] == '-') && i > 0 {
			left := evalSimpleMath(expr[:i])
			right := evalSimpleMath(expr[i+1:])
			if expr[i] == '+' {
				return left + right
			}
			return left - right
		}
	}
	// find * or /
	for i := len(expr) - 1; i >= 0; i-- {
		if expr[i] == '*' || expr[i] == '/' {
			left := evalSimpleMath(expr[:i])
			right := evalSimpleMath(expr[i+1:])
			if expr[i] == '*' {
				return left * right
			}
			if right != 0 {
				return left / right
			}
		}
	}
	return 0
}

func isAssignment(line string) bool {
	if strings.Contains(line, "→") {
		return false
	}
	eqIdx := strings.Index(line, "=")
	if eqIdx < 0 {
		return false
	}
	if eqIdx > 0 && (line[eqIdx-1] == '>' || line[eqIdx-1] == '<' || line[eqIdx-1] == '!') {
		return false
	}
	if eqIdx+1 < len(line) && line[eqIdx+1] == '=' {
		return false
	}
	varName := strings.TrimSpace(line[:eqIdx])
	// must be single word
	if strings.Contains(varName, " ") {
		return false
	}
	if len(varName) == 0 {
		return false
	}
	// must start with letter or underscore
	first := varName[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}
	// block command keywords that start a line (must be followed by space)
	blockedCommands := []string{
		"go", "find", "check", "remove", "add", "compare",
		"divide", "start", "print", "say", "repeat",
		"define", "ask", "save", "load", "send", "increase",
		"decrease", "append", "set", "run", "if", "else",
		"multiply", "subtract", "calculate", "get", "make",
		"convert", "split", "join", "replace", "call", "fetch",
		"sort", "reverse", "wait", "for", "return", "while",
		"random", "round", "absolute", "power", "format",
		"combine",
	}
	for _, cmd := range blockedCommands {
		// block "cmd " (command with space after = has args) or exact match
		if strings.HasPrefix(line, cmd+" ") {
			return false
		}
	}
	exactBlocked := []string{"stop", "break", "continue", "done"}
	for _, kw := range exactBlocked {
		if varName == kw {
			return false
		}
	}
	return true
}

func matchLine(pattern, line string) []string {
	re := regexp.MustCompile(`(?i)` + pattern)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	// pad to at least 5 elements
	for len(m) < 5 {
		m = append(m, "")
	}
	return m
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ─── NEW FEATURES v1.1 ────────────────────────────────────────────────────────
// Added via patch — all new commands handled in execLineV2 which is called
// before falling through to the original execLine logic.
// This function returns (jumpIdx, handled, error)

func (interp *Interpreter) execLineV2(line string, lines []string, idx, end int) (int, bool, error) {
	var m []string

	// ── while <condition> ─────────────────────────────────────────────────────
	m = matchLine(`^while\s+(.+)$`, line)
	if m != nil {
		condStr := m[1]
		body, endIdx := interp.collectBlock(lines, idx, end)
		maxIter := 10000 // safety limit
		iter := 0
		for iter < maxIter {
			// eval condition using current interp variables
			if !interp.evalCondition(condStr) {
				break
			}
			// run body directly on interp so variable updates persist
			interp.executeLines(body, 0, len(body))
			iter++
		}
		return endIdx + 1, true, nil
	}

	// ── else if <condition> → outcome ─────────────────────────────────────────
	m = matchLine(`^else\s+if\s+(.+?)\s*→\s*(.+)$`, line)
	if m != nil {
		if !interp.LastCondition {
			result := interp.evalCondition(m[1])
			interp.LastCondition = result
			if result { interp.doOutcome(m[2]) }
		}
		return -1, true, nil
	}

	// ── break / continue (standalone) ─────────────────────────────────────────
	if line == "break" || line == "continue" {
		return -1, true, nil // handled by loop
	}

	// ── RANDOM ────────────────────────────────────────────────────────────────

	// random number between X and Y into var
	m = matchLine(`^random\s+number\s+between\s+(\S+)\s+and\s+(\S+)\s+into\s+(\w+)$`, line)
	if m != nil {
		import_rand_once()
		min_, _ := toFloat(interp.resolve(m[1]))
		max_, _ := toFloat(interp.resolve(m[2]))
		result := min_ + randFloat()*(max_-min_)
		if result == math.Trunc(result) {
			interp.Variables[m[3]] = int(result)
		} else {
			interp.Variables[m[3]] = result
		}
		return -1, true, nil
	}

	// random integer between X and Y into var
	m = matchLine(`^random\s+integer\s+between\s+(\S+)\s+and\s+(\S+)\s+into\s+(\w+)$`, line)
	if m != nil {
		import_rand_once()
		min_, _ := toFloat(interp.resolve(m[1]))
		max_, _ := toFloat(interp.resolve(m[2]))
		result := int(min_) + randInt(int(max_)-int(min_)+1)
		interp.Variables[m[3]] = result
		return -1, true, nil
	}

	// ── MATH EXTRAS ───────────────────────────────────────────────────────────

	// round <var> to <n> decimals into <result>
	m = matchLine(`^round\s+(\w+)\s+to\s+(\d+)\s+decimals?\s+into\s+(\w+)$`, line)
	if m != nil {
		val, _ := toFloat(interp.resolve(m[1]))
		n, _ := strconv.Atoi(m[2])
		factor := math.Pow(10, float64(n))
		result := math.Round(val*factor) / factor
		if n == 0 { interp.Variables[m[3]] = int(result) } else { interp.Variables[m[3]] = result }
		return -1, true, nil
	}

	// round <var> into <result>  (round to nearest int)
	m = matchLine(`^round\s+(\w+)\s+into\s+(\w+)$`, line)
	if m != nil {
		val, _ := toFloat(interp.resolve(m[1]))
		interp.Variables[m[2]] = int(math.Round(val))
		return -1, true, nil
	}

	// absolute value of <var> into <result>
	m = matchLine(`^absolute\s+(?:value\s+of\s+)?(\w+)\s+into\s+(\w+)$`, line)
	if m != nil {
		val, _ := toFloat(interp.resolve(strings.TrimSpace(m[1])))
		result := math.Abs(val)
		if result == math.Trunc(result) {
			interp.Variables[m[2]] = int(result)
		} else {
			interp.Variables[m[2]] = result
		}
		return -1, true, nil
	}

	// power of <base> to <exp> into <result>
	m = matchLine(`^power\s+of\s+(\S+)\s+to\s+(\S+)\s+into\s+(\w+)$`, line)
	if m != nil {
		base, _ := toFloat(interp.resolve(m[1]))
		exp_, _ := toFloat(interp.resolve(m[2]))
		result := math.Pow(base, exp_)
		if result == math.Trunc(result) { interp.Variables[m[3]] = int(result) } else { interp.Variables[m[3]] = result }
		return -1, true, nil
	}

	// format <var> to <n> decimals into <result>
	m = matchLine(`^format\s+(\w+)\s+to\s+(\d+)\s+decimals?\s+into\s+(\w+)$`, line)
	if m != nil {
		val, _ := toFloat(interp.resolve(m[1]))
		n := m[2]
		interp.Variables[m[3]] = fmt.Sprintf("%."+n+"f", val)
		return -1, true, nil
	}

	// ── STRING EXTRAS ─────────────────────────────────────────────────────────

	// check if <var> starts with "<prefix>"
	m = matchLine(`^check\s+if\s+(\w+)\s+starts\s+with\s+"(.+)"$`, line)
	if m != nil {
		val := toString(interp.resolve(m[1]))
		interp.LastCondition = strings.HasPrefix(val, m[2])
		return -1, true, nil
	}

	// check if <var> ends with "<suffix>"
	m = matchLine(`^check\s+if\s+(\w+)\s+ends\s+with\s+"(.+)"$`, line)
	if m != nil {
		val := toString(interp.resolve(m[1]))
		interp.LastCondition = strings.HasSuffix(val, m[2])
		return -1, true, nil
	}

	// check if <var> contains "<text>"
	m = matchLine(`^check\s+if\s+(\w+)\s+contains\s+"(.+)"$`, line)
	if m != nil {
		val := toString(interp.resolve(m[1]))
		interp.LastCondition = strings.Contains(val, m[2])
		return -1, true, nil
	}

	// get length of <var> into <result>
	m = matchLine(`^get\s+length\s+of\s+(\w+)\s+into\s+(\w+)$`, line)
	if m != nil {
		val := interp.resolve(m[1])
		if lst, ok := val.([]Value); ok {
			interp.Variables[m[2]] = len(lst)
		} else {
			interp.Variables[m[2]] = len(toString(val))
		}
		return -1, true, nil
	}

	// find "<text>" in <var> into <result>  (returns position, -1 if not found)
	m = matchLine(`^find\s+"(.+?)"\s+in\s+(\w+)\s+into\s+(\w+)$`, line)
	if m != nil {
		val := toString(interp.resolve(m[2]))
		pos := strings.Index(val, m[1])
		interp.Variables[m[3]] = pos
		return -1, true, nil
	}

	// combine <a> and <b> into <result>
	m = matchLine(`^combine\s+(.+?)\s+and\s+(.+?)\s+into\s+(\w+)$`, line)
	if m != nil {
		a := toString(interp.resolveExpr(m[1]))
		b := toString(interp.resolveExpr(m[2]))
		interp.Variables[m[3]] = a + b
		return -1, true, nil
	}

	// repeat string <var> <n> times into <result>
	m = matchLine(`^repeat\s+string\s+(\w+)\s+(\S+)\s+times\s+into\s+(\w+)$`, line)
	if m != nil {
		val := toString(interp.resolve(m[1]))
		n, _ := strconv.Atoi(m[2])
		interp.Variables[m[3]] = strings.Repeat(val, n)
		return -1, true, nil
	}

	// ── LIST EXTRAS ───────────────────────────────────────────────────────────

	// get item <n> from <list> into <result>
	m = matchLine(`^get\s+item\s+(\S+)\s+from\s+(\w+)\s+into\s+(\w+)$`, line)
	if m != nil {
		n, _ := strconv.Atoi(m[1])
		lst, ok := interp.resolve(m[2]).([]Value)
		if ok && n >= 1 && n <= len(lst) {
			interp.Variables[m[3]] = lst[n-1]
		} else if ok && n == 0 {
			interp.Variables[m[3]] = lst[0]
		} else {
			interp.Variables[m[3]] = nil
		}
		return -1, true, nil
	}

	// check if <list> contains <value>
	m = matchLine(`^check\s+if\s+(\w+)\s+contains\s+(\S+)$`, line)
	if m != nil {
		lst, ok := interp.resolve(m[1]).([]Value)
		needle := toString(interp.resolve(m[2]))
		found := false
		if ok {
			for _, item := range lst {
				if toString(item) == needle { found = true; break }
			}
		}
		interp.LastCondition = found
		return -1, true, nil
	}

	// remove item <n> from <list>
	m = matchLine(`^remove\s+item\s+(\S+)\s+from\s+(\w+)$`, line)
	if m != nil {
		n, _ := strconv.Atoi(m[1])
		lst, ok := interp.Variables[m[2]].([]Value)
		if ok && n >= 1 && n <= len(lst) {
			interp.Variables[m[2]] = append(lst[:n-1], lst[n:]...)
			interp.CurrentList = interp.Variables[m[2]].([]Value)
		}
		return -1, true, nil
	}

	// remove value "<x>" from <list>
	m = matchLine(`^remove\s+value\s+"?(.+?)"?\s+from\s+(\w+)$`, line)
	if m != nil {
		needle := m[1]
		lst, ok := interp.Variables[m[2]].([]Value)
		if ok {
			var result []Value
			for _, item := range lst {
				if toString(item) != needle { result = append(result, item) }
			}
			interp.Variables[m[2]] = result
			interp.CurrentList = result
		}
		return -1, true, nil
	}

	// get length of list <var> into <result>  (alias)
	m = matchLine(`^get\s+list\s+length\s+of\s+(\w+)\s+into\s+(\w+)$`, line)
	if m != nil {
		lst, ok := interp.resolve(m[1]).([]Value)
		if ok { interp.Variables[m[2]] = len(lst) } else { interp.Variables[m[2]] = 0 }
		return -1, true, nil
	}

	// check if list <var> is empty
	m = matchLine(`^check\s+if\s+(\w+)\s+is\s+empty$`, line)
	if m != nil {
		val := interp.resolve(m[1])
		if lst, ok := val.([]Value); ok {
			interp.LastCondition = len(lst) == 0
		} else {
			interp.LastCondition = val == nil || toString(val) == ""
		}
		return -1, true, nil
	}

	// ── OUTPUT EXTRAS ─────────────────────────────────────────────────────────

	// print <var> with <n> decimals
	m = matchLine(`^print\s+(\w+)\s+with\s+(\d+)\s+decimals?$`, line)
	if m != nil {
		val, _ := toFloat(interp.resolve(m[1]))
		n := m[2]
		fmt.Printf("  "+fmt.Sprintf("%."+n+"f", val)+"\n")
		return -1, true, nil
	}

	// say "<text>" in green/red/yellow/blue
	m = matchLine(`^say\s+"(.+?)"\s+in\s+(green|red|yellow|blue)$`, line)
	if m != nil {
		colors := map[string]string{
			"green": "\033[32m", "red": "\033[31m",
			"yellow": "\033[33m", "blue": "\033[34m",
		}
		reset := "\033[0m"
		fmt.Printf("  %s%s%s\n", colors[m[2]], m[1], reset)
		return -1, true, nil
	}

	return -1, false, nil
}

// random helpers (no import needed, use math/rand via time seed)
var _randSeeded bool
var _randSource *pseudoRand

type pseudoRand struct{ state uint64 }

func (r *pseudoRand) Float() float64 {
	r.state ^= r.state << 13
	r.state ^= r.state >> 7
	r.state ^= r.state << 17
	return float64(r.state&0x7FFFFFFFFFFFFFFF) / float64(0x7FFFFFFFFFFFFFFF)
}

func (r *pseudoRand) Int(n int) int {
	if n <= 0 { return 0 }
	return int(r.Float() * float64(n))
}

func import_rand_once() {
	if !_randSeeded {
		_randSource = &pseudoRand{state: uint64(time.Now().UnixNano())}
		_randSeeded = true
	}
}

func randFloat() float64 {
	if _randSource == nil { import_rand_once() }
	return _randSource.Float()
}

func randInt(n int) int {
	if _randSource == nil { import_rand_once() }
	return _randSource.Int(n)
}
