package contenttype

import (
	"bytes"
	"fmt"
	"testing"
)

type ParseContentTypeTestCase struct {
	Input          string
	ExpectedOutput *ContentType /* or nil if an error is expected */
	ExpectedString *string      /* or nil if equals to Input */
}

var parseContentTypeTestCases = []ParseContentTypeTestCase{
	{
		"text/html",
		&ContentType{"text", "html", "", map[string]string{}},
		nil,
	},
	{
		"text/svg+xml; charset=UTF-8",
		&ContentType{"text", "svg", "xml", map[string]string{"charset": "UTF-8"}},
		nil,
	},
	{
		"text/",
		nil,
		nil,
	},
	{
		"text; charset=UTF-8",
		&ContentType{"text", "", "", map[string]string{"charset": "UTF-8"}},
		nil,
	},
	{
		"text/+xml; charset=UTF-8",
		&ContentType{"text", "", "xml", map[string]string{"charset": "UTF-8"}},
		nil,
	},
}

type ContentTypeEqualsTestCase struct {
	A, B   ContentType
	Equals bool
}

var MapEmpty = map[string]string{}
var MapA = map[string]string{"a": "value_a"}
var MapB = map[string]string{"b": "value_b"}
var MapAb = map[string]string{"a": "value_a", "b": "value_b"}

var ContentTypeE = ContentType{"a", "b", "c", MapEmpty}
var ContentTypeA = ContentType{"a", "b", "c", MapA}
var ContentTypeB = ContentType{"a", "b", "c", MapB}
var ContentTypeAb = ContentType{"a", "b", "c", MapAb}

var contentTypeEqualsTestCases = []ContentTypeEqualsTestCase{
	// TopLevelType, SubType, Suffix
	{ContentTypeE, ContentType{"a", "b", "c", MapEmpty}, true},
	{ContentTypeE, ContentType{"o", "b", "c", MapEmpty}, false},
	{ContentTypeE, ContentType{"a", "o", "c", MapEmpty}, false},
	{ContentTypeE, ContentType{"a", "b", "o", MapEmpty}, false},
	// Parameters
	{ContentTypeA, ContentTypeA, true},
	{ContentTypeB, ContentTypeB, true},
	{ContentTypeAb, ContentTypeAb, true},
	{ContentTypeA, ContentTypeE, false},
	{ContentTypeA, ContentTypeB, false},
	{ContentTypeB, ContentTypeA, false},
	{ContentTypeAb, ContentTypeA, false},
	{ContentTypeAb, ContentTypeE, false},
	{ContentTypeA, ContentTypeAb, false},
}

type FilterTestCase struct {
	Description string
	Input       Filter
	TrueValues  []ContentType
	FalseValues []ContentType
}

var filterTestCases = []FilterTestCase{
	{
		"contains xml",
		NewFilterContains("xml"),
		[]ContentType{
			{"xml", "", "", MapEmpty},
			{"text", "xml", "", MapEmpty},
			{"text", "html", "xml", MapEmpty},
		},
		[]ContentType{
			{"text", "svg", "", map[string]string{"script": "javascript"}},
			{"java", "script", "", MapEmpty},
		},
	},
	{
		"equals applications/xhtml",
		NewFilterEquals("application", "xhtml", "*"),
		[]ContentType{
			{"application", "xhtml", "xml", MapEmpty},
			{"application", "xhtml", "", MapEmpty},
			{"application", "xhtml", "zip", MapEmpty},
			{"application", "xhtml", "zip", MapAb},
		},
		[]ContentType{
			{"application", "javascript", "", MapEmpty},
			{"text", "xhtml", "", MapEmpty},
		},
	},
	{
		"equals application/*",
		NewFilterEquals("application", "*", ""),
		[]ContentType{
			{"application", "xhtml", "", MapEmpty},
			{"application", "javascript", "", MapEmpty},
		},
		[]ContentType{
			{"text", "xhtml", "", MapEmpty},
			{"text", "xhtml", "xml", MapEmpty},
		},
	},
	{
		"equals applications */javascript",
		NewFilterEquals("*", "javascript", ""),
		[]ContentType{
			{"application", "javascript", "", MapEmpty},
			{"text", "javascript", "", MapEmpty},
		},
		[]ContentType{
			{"text", "html", "", MapEmpty},
			{"text", "javascript", "zip", MapEmpty},
		},
	},
	{
		"equals applications/* or */javascript",
		NewFilterOr([]Filter{
			NewFilterEquals("application", "*", ""),
			NewFilterEquals("*", "javascript", ""),
		}),
		[]ContentType{
			{"application", "javascript", "", MapEmpty},
			{"text", "javascript", "", MapEmpty},
			{"application", "xhtml", "", MapEmpty},
		},
		[]ContentType{
			{"text", "html", "", MapEmpty},
			{"application", "xhtml", "xml", MapEmpty},
		},
	},
}

type FilterParametersTestCase struct {
	Input  map[string]string
	Filter map[string]bool
	Output map[string]string
}

var filterParametersTestCases = []FilterParametersTestCase{
	{
		map[string]string{},
		map[string]bool{"A": true, "B": true},
		map[string]string{},
	},
	{
		map[string]string{"A": "value_A", "B": "value_B"},
		map[string]bool{},
		map[string]string{},
	},
	{
		map[string]string{"A": "value_A", "B": "value_B"},
		map[string]bool{"A": true},
		map[string]string{"A": "value_A"},
	},
	{
		map[string]string{"A": "value_A", "B": "value_B"},
		map[string]bool{"A": true, "B": true},
		map[string]string{"A": "value_A", "B": "value_B"},
	},
}

func TestContentTypeEquals(t *testing.T) {
	for _, testCase := range contentTypeEqualsTestCases {
		if !testCase.A.Equals(testCase.B) && testCase.Equals {
			t.Errorf(`Must be equals "%s"="%s"`, testCase.A, testCase.B)
		} else if testCase.A.Equals(testCase.B) && !testCase.Equals {
			t.Errorf(`Mustn't be equals "%s"!="%s"`, testCase.A, testCase.B)
		}
	}
}

func TestParseContentType(t *testing.T) {
	for _, testCase := range parseContentTypeTestCases {
		// test ParseContentType
		contentType, err := ParseContentType(testCase.Input)
		if testCase.ExpectedOutput == nil {
			// error expected
			if err == nil {
				// but there is no error
				t.Errorf(`Expecting error for "%s"`, testCase.Input)
			}
		} else {
			// no expected error
			if err != nil {
				t.Errorf(`Unexpecting error for "%s" : %s`, testCase.Input, err)
			} else if !contentType.Equals(*testCase.ExpectedOutput) {
				// the parsed contentType doesn't matched
				t.Errorf(`Unexpecting result for "%s", instead got "%s"`, testCase.ExpectedOutput.String(), contentType.String())
			} else {
				// ParseContentType is fine, checking String()
				contentTypeString := contentType.String()
				expectedString := testCase.Input
				if testCase.ExpectedString != nil {
					expectedString = *testCase.ExpectedString
				}
				if contentTypeString != expectedString {
					t.Errorf(`Error with String() output of "%s", got "%s", ContentType{"%s", "%s", "%s", "%s"}`, expectedString, contentTypeString, contentType.TopLevelType, contentType.SubType, contentType.Suffix, contentType.Parameters)
				}
			}
		}
	}
}

func FilterToString(m map[string]bool) string {
	b := new(bytes.Buffer)
	for key, value := range m {
		if value {
			_, _ = fmt.Fprintf(b, "'%s'=true;", key)
		} else {
			_, _ = fmt.Fprintf(b, "'%s'=false;", key)
		}
	}
	return b.String()
}

func TestFilters(t *testing.T) {
	for _, testCase := range filterTestCases {
		for _, contentType := range testCase.TrueValues {
			if !testCase.Input(contentType) {
				t.Errorf(`Filter "%s" must accept the value "%s"`, testCase.Description, contentType)
			}
		}
		for _, contentType := range testCase.FalseValues {
			if testCase.Input(contentType) {
				t.Errorf(`Filter "%s" mustn't accept the value "%s"`, testCase.Description, contentType)
			}
		}
	}
}

func TestFilterParameters(t *testing.T) {
	for _, testCase := range filterParametersTestCases {
		// copy Input since the map will be modified
		InputCopy := make(map[string]string)
		for k, v := range testCase.Input {
			InputCopy[k] = v
		}
		// apply filter
		contentType := ContentType{"", "", "", InputCopy}
		contentType.FilterParameters(testCase.Filter)
		// test
		contentTypeOutput := ContentType{"", "", "", testCase.Output}
		if !contentTypeOutput.Equals(contentType) {
			t.Errorf(`FilterParameters error : %s becomes %s with this filter %s`, testCase.Input, contentType.Parameters, FilterToString(testCase.Filter))
		}
	}
}
