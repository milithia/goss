package matchers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"

	"github.com/onsi/gomega/format"
)

const (
	maxScanTokenSize = 1024 * 1024
)

type HavePatternsMatcher struct {
	Elements        interface{}
	missingElements []string
}

//FIXME
//ContainElements succeeds if actual contains the passed in elements. The ordering of the elements does not matter.
//By default ContainElements() uses Equal() to match the elements, however custom matchers can be passed in instead. Here are some examples:
//
//    Expect([]string{"Foo", "FooBar"}).Should(ContainElements("FooBar"))
//    Expect([]string{"Foo", "FooBar"}).Should(ContainElements(ContainSubstring("Bar"), "Foo"))
//
//Actual must be an array, slice or map.
//For maps, ContainElements searches through the map's values.
func HavePatterns(elements interface{}) GossMatcher {
	return &HavePatternsMatcher{
		Elements: elements,
	}
}

func (m *HavePatternsMatcher) Match(actual interface{}) (success bool, err error) {
	t, ok := m.Elements.([]interface{})
	if !ok {
		return false, fmt.Errorf("HavePatterns matcher expects an io.reader.  Got:\n%s", format.Object(actual, 1))
	}
	elements := make([]string, len(t))
	for i, v := range t {
		switch v := v.(type) {
		case string:
			elements[i] = v
		default:
			return false, fmt.Errorf("HavePatterns matcher expects patterns to be a string. got: \n%s", format.Object(v, 1))
		}
	}
	notfound, err := sliceToPatterns(elements)
	// short circuit
	if len(notfound) == 0 && err == nil {
		return true, nil
	}
	fh, ok := actual.(io.Reader)
	if !ok {
		return false, fmt.Errorf("Incorrect type")
	}

	defer func() {
		if rc, ok := fh.(io.ReadCloser); ok {
			rc.Close()
		}
	}()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(nil, maxScanTokenSize)
	var found []patternMatcher
	for scanner.Scan() {
		line := scanner.Text()

		i := 0
		for _, pat := range notfound {
			if pat.Match(line) {
				// Found it, but wasn't supposed to, don't mark it as found, but remove it from search
				if !pat.Inverse() {
					found = append(found, pat)
				}
				continue
			}
			notfound[i] = pat
			i++
		}
		notfound = notfound[:i]
		if len(notfound) == 0 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}

	for _, pat := range notfound {
		// Didn't find it, but we didn't want to.. so we mark it as found
		// Empty pattern should match even if input to scanner is empty
		if pat.Inverse() || pat.Pattern() == "" {
			found = append(found, pat)
		}
	}

	if len(elements) != len(found) {
		found := patternsToSlice(found)
		m.missingElements = subtractSlice(elements, found)
		return false, nil
	}
	return true, nil
}

func (m *HavePatternsMatcher) FailureResult(actual interface{}) MatcherResult {
	return MatcherResult{
		Actual:          fmt.Sprintf("object: %T", actual),
		Message:         "to contain patterns",
		Expected:        m.Elements,
		MissingElements: m.missingElements,
	}
}

func (m *HavePatternsMatcher) NegatedFailureResult(actual interface{}) MatcherResult {
	return MatcherResult{
		Actual:   fmt.Sprintf("object: %T", actual),
		Message:  "not to contain patterns",
		Expected: m.Elements,
	}
}

func (m *HavePatternsMatcher) FailureMessage(actual interface{}) (message string) {
	message = format.Message(reflect.TypeOf(actual), "to contain elements", m.Elements)
	return appendMissingStrings(message, m.missingElements)
}

func (m *HavePatternsMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to contain elements", m.Elements)
}

func appendMissingStrings(message string, missingElements []string) string {
	if len(missingElements) == 0 {
		return message
	}
	return fmt.Sprintf("%s\nthe missing elements were\n%s", message,
		format.Object(missingElements, 1))
}

type patternMatcher interface {
	Match(string) bool
	Pattern() string
	Inverse() bool
}

type stringPattern struct {
	pattern      string
	cleanPattern string
	inverse      bool
}

func newStringPattern(str string) *stringPattern {
	var inverse bool
	if strings.HasPrefix(str, "!") {
		inverse = true
	}
	cleanPattern := strings.TrimLeft(str, "\\/!")
	return &stringPattern{
		pattern:      str,
		cleanPattern: cleanPattern,
		inverse:      inverse,
	}
}

func (s *stringPattern) Match(str string) bool {
	return strings.Contains(str, s.cleanPattern)
}

func (s *stringPattern) Pattern() string { return s.pattern }
func (s *stringPattern) Inverse() bool   { return s.inverse }

type regexPattern struct {
	pattern string
	re      *regexp.Regexp
	inverse bool
}

func newRegexPattern(str string) (*regexPattern, error) {
	var inverse bool
	cleanStr := str
	if strings.HasPrefix(str, "!") {
		inverse = true
		cleanStr = cleanStr[1:]
	}
	trimLeft := []rune{'\\', '/'}
	for _, r := range trimLeft {
		if rune(cleanStr[0]) == r {
			cleanStr = cleanStr[1:]
			break
		}
	}
	trimRight := []rune{'/'}
	for _, r := range trimRight {
		if rune(cleanStr[len(cleanStr)-1]) == r {
			cleanStr = cleanStr[:len(cleanStr)-1]
			break
		}
	}

	re, err := regexp.Compile(cleanStr)

	return &regexPattern{
		pattern: str,
		re:      re,
		inverse: inverse,
	}, err

}

func (re *regexPattern) Match(str string) bool {
	return re.re.MatchString(str)
}

func (re *regexPattern) Pattern() string { return re.pattern }
func (re *regexPattern) Inverse() bool   { return re.inverse }

func sliceToPatterns(slice []string) ([]patternMatcher, error) {
	var patterns []patternMatcher
	for _, s := range slice {
		if (strings.HasPrefix(s, "/") || strings.HasPrefix(s, "!/")) && strings.HasSuffix(s, "/") {
			pat, err := newRegexPattern(s)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, pat)
		} else {
			patterns = append(patterns, newStringPattern(s))
		}
	}
	return patterns, nil
}

func patternsToSlice(patterns []patternMatcher) []string {
	var slice []string
	for _, p := range patterns {
		slice = append(slice, p.Pattern())
	}
	return slice
}
func subtractSlice(x, y []string) []string {
	m := make(map[string]bool)

	for _, y := range y {
		m[y] = true
	}

	var ret []string
	for _, x := range x {
		if m[x] {
			continue
		}
		ret = append(ret, x)
	}

	return ret
}

func (matcher *HavePatternsMatcher) MarshalJSON() ([]byte, error) {
	j := make(map[string]interface{})
	j["have-patterns"] = matcher.Elements
	return json.Marshal(j)
}

func (matcher *HavePatternsMatcher) String() string {
	return format.Object(matcher, 0)
}
