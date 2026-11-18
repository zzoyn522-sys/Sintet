package workshop

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	maven "github.com/masahiro331/go-mvn-version"
)

type versionRange func(string) bool

var fabricVersionSyntax = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)*)(?:-([0-9A-Za-z.-]*))?(?:\+[0-9A-Za-z.-]+)?$`)
var numericComponent = regexp.MustCompile(`^[0-9]+$`)

type fabricVersion struct {
	core    []string
	pre     []string
	release bool
}

func parseFabricVersion(raw string) (fabricVersion, bool) {
	fields := fabricVersionSyntax.FindStringSubmatchIndex(raw)
	if fields == nil {
		return fabricVersion{}, false
	}
	v := fabricVersion{core: strings.Split(raw[fields[2]:fields[3]], "."), release: fields[4] < 0}
	if !v.release {
		v.pre = strings.Split(raw[fields[4]:fields[5]], ".")
	}
	return v, true
}

func compareDigits(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func fabricComponent(v fabricVersion, index int) string {
	if index >= len(v.core) {
		return "0"
	}
	return v.core[index]
}

func compareFabric(a, b fabricVersion) int {
	for i := 0; i < max(len(a.core), len(b.core)); i++ {
		if n := compareDigits(fabricComponent(a, i), fabricComponent(b, i)); n != 0 {
			return n
		}
	}
	if a.release != b.release {
		if a.release {
			return 1
		}
		return -1
	}
	for i := 0; i < min(len(a.pre), len(b.pre)); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		if x == "" {
			return -1
		}
		if y == "" {
			return 1
		}
		ax, by := numericComponent.MatchString(x), numericComponent.MatchString(y)
		if ax && by {
			if n := compareDigits(x, y); n != 0 {
				return n
			}
		} else if ax {
			return -1
		} else if by {
			return 1
		} else {
			return strings.Compare(x, y)
		}
	}
	return len(a.pre) - len(b.pre)
}

func parseFabricRange(raw json.RawMessage) (versionRange, error) {
	var single string
	var options []string
	if json.Unmarshal(raw, &single) == nil && string(raw) != "null" {
		options = []string{single}
	} else if err := json.Unmarshal(raw, &options); err != nil || len(options) == 0 {
		return nil, fmt.Errorf("неверный диапазон Fabric: %s", raw)
	}
	alternatives := make([][]versionRange, 0, len(options))
	for _, option := range options {
		var predicates []versionRange
		for _, token := range strings.Fields(option) {
			fn, err := parseFabricPredicate(token)
			if err != nil {
				return nil, err
			}
			predicates = append(predicates, fn)
		}
		if len(predicates) == 0 {
			return nil, fmt.Errorf("пустой диапазон Fabric")
		}
		alternatives = append(alternatives, predicates)
	}
	return func(v string) bool {
		return slices.ContainsFunc(alternatives, func(group []versionRange) bool {
			return !slices.ContainsFunc(group, func(predicate versionRange) bool { return !predicate(v) })
		})
	}, nil
}

func parseFabricPredicate(token string) (versionRange, error) {
	op, raw := "=", token
	for _, prefix := range []string{">=", "<=", ">", "<", "=", "~", "^"} {
		if rest, ok := strings.CutPrefix(token, prefix); ok {
			op, raw = prefix, rest
			break
		}
	}
	if raw == "*" && op == "=" {
		return func(string) bool { return true }, nil
	}
	if raw == "" {
		return nil, fmt.Errorf("неверный предикат Fabric: %s", token)
	}
	wildcard := strings.Split(raw, ".")
	for i, part := range wildcard {
		if part != "*" && part != "x" && part != "X" {
			continue
		}
		if op != "=" || i == 0 {
			return nil, fmt.Errorf("неверный предикат Fabric: %s", token)
		}
		for j, component := range wildcard {
			if (j < i && !numericComponent.MatchString(component)) || (j >= i && component != "x" && component != "X" && component != "*") {
				return nil, fmt.Errorf("неверный предикат Fabric: %s", token)
			}
		}
		return func(raw string) bool {
			v, ok := parseFabricVersion(raw)
			if !ok {
				return false
			}
			for j := 0; j < i; j++ {
				if compareDigits(fabricComponent(v, j), wildcard[j]) != 0 {
					return false
				}
			}
			return true
		}, nil
	}
	want, semantic := parseFabricVersion(raw)
	if !semantic && op != "=" {
		return nil, fmt.Errorf("неверная версия Fabric: %s", raw)
	}
	return func(candidate string) bool {
		actual, ok := parseFabricVersion(candidate)
		if !ok || !semantic {
			return op == "=" && candidate == raw
		}
		n := compareFabric(actual, want)
		switch op {
		case ">=":
			return n >= 0
		case "<=":
			return n <= 0
		case ">":
			return n > 0
		case "<":
			return n < 0
		case "^":
			return n >= 0 && compareDigits(fabricComponent(actual, 0), fabricComponent(want, 0)) == 0
		case "~":
			return n >= 0 && compareDigits(fabricComponent(actual, 0), fabricComponent(want, 0)) == 0 && compareDigits(fabricComponent(actual, 1), fabricComponent(want, 1)) == 0
		default:
			return n == 0
		}
	}, nil
}

type mavenInterval struct {
	min, max                               maven.Version
	hasMin, hasMax, includeMin, includeMax bool
}

func parseMavenRange(raw string) (versionRange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return func(string) bool { return true }, nil
	}
	if !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "(") {
		// A bare Maven version is a recommendation, with no restrictive interval.
		if strings.ContainsAny(raw, "[](),") {
			return nil, fmt.Errorf("неверный интервал Maven: %s", raw)
		}
		return func(string) bool { return true }, nil
	}
	var intervals []mavenInterval
	for raw != "" {
		end := strings.IndexAny(raw, "])")
		if end < 1 || (raw[0] != '[' && raw[0] != '(') {
			return nil, fmt.Errorf("неверный интервал Maven: %s", raw)
		}
		left, right, pair := strings.Cut(raw[1:end], ",")
		left, right = strings.TrimSpace(left), strings.TrimSpace(right)
		i := mavenInterval{includeMin: raw[0] == '[', includeMax: raw[end] == ']'}
		if !pair {
			if !i.includeMin || !i.includeMax || left == "" {
				return nil, fmt.Errorf("неверный интервал Maven: %s", raw)
			}
			right = left
		}
		if strings.ContainsAny(left+right, "[](),") || left == "" && right == "" {
			return nil, fmt.Errorf("неверный интервал Maven: %s", raw)
		}
		var err error
		if left != "" {
			i.min, err = maven.NewVersion(left)
			if err != nil {
				return nil, err
			}
			i.hasMin = true
		}
		if right != "" {
			i.max, err = maven.NewVersion(right)
			if err != nil {
				return nil, err
			}
			i.hasMax = true
		}
		intervals = append(intervals, i)
		raw = strings.TrimSpace(raw[end+1:])
		if raw != "" {
			tail, ok := strings.CutPrefix(raw, ",")
			raw = strings.TrimSpace(tail)
			if !ok || raw == "" {
				return nil, fmt.Errorf("неверное объединение интервалов Maven")
			}
		}
	}
	return func(raw string) bool {
		v, err := maven.NewVersion(raw)
		if err != nil || raw == "" {
			return false
		}
		return slices.ContainsFunc(intervals, func(i mavenInterval) bool {
			lower, upper := 0, 0
			if i.hasMin {
				lower = v.Compare(i.min)
			}
			if i.hasMax {
				upper = v.Compare(i.max)
			}
			return (!i.hasMin || lower > 0 || lower == 0 && i.includeMin) && (!i.hasMax || upper < 0 || upper == 0 && i.includeMax)
		})
	}, nil
}
