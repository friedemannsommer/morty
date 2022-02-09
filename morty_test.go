package main

import (
	"bytes"
	"net/url"
	"testing"
)

type AttrTestCase struct {
	AttrName       []byte
	AttrValue      []byte
	ExpectedOutput []byte
}

type SanitizeURITestCase struct {
	Input          []byte
	ExpectedOutput []byte
	ExpectedScheme string
}

type StringTestCase struct {
	Input          string
	ExpectedOutput string
}

var attrTestData = []*AttrTestCase{
	{
		[]byte("href"),
		[]byte("./x"),
		[]byte(` href="./?mortyurl=http%3A%2F%2F127.0.0.1%2Fx"`),
	},
	{
		[]byte("src"),
		[]byte("http://x.com/y"),
		[]byte(` src="./?mortyurl=http%3A%2F%2Fx.com%2Fy"`),
	},
	{
		[]byte("action"),
		[]byte("/z"),
		[]byte(` action="./?mortyurl=http%3A%2F%2F127.0.0.1%2Fz"`),
	},
	{
		[]byte("onclick"),
		[]byte("console.log(document.cookies)"),
		nil,
	},
}

var sanitizeUriTestData = []*SanitizeURITestCase{
	{
		[]byte("http://example.com/"),
		[]byte("http://example.com/"),
		"http:",
	},
	{
		[]byte("HtTPs://example.com/     \t"),
		[]byte("https://example.com/"),
		"https:",
	},
	{
		[]byte("      Ht  TPs://example.com/     \t"),
		[]byte("https://example.com/"),
		"https:",
	},
	{
		[]byte("javascript:void(0)"),
		[]byte("javascript:void(0)"),
		"javascript:",
	},
	{
		[]byte("      /path/to/a/file/without/protocol     "),
		[]byte("/path/to/a/file/without/protocol"),
		"",
	},
	{
		[]byte("      #fragment     "),
		[]byte("#fragment"),
		"",
	},
	{
		[]byte("      qwertyuiop     "),
		[]byte("qwertyuiop"),
		"",
	},
	{
		[]byte(""),
		[]byte(""),
		"",
	},
	{
		[]byte(":"),
		[]byte(":"),
		":",
	},
	{
		[]byte("   :"),
		[]byte(":"),
		":",
	},
	{
		[]byte("schéma:"),
		[]byte("schéma:"),
		"schéma:",
	},
}

var urlTestData = []*StringTestCase{
	{
		"http://x.com/",
		"./?mortyurl=http%3A%2F%2Fx.com%2F",
	},
	{
		"http://a@x.com/",
		"./?mortyurl=http%3A%2F%2Fa%40x.com%2F",
	},
	{
		"#a",
		"#a",
	},
}

func TestAttrSanitizer(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1/")
	rc := &RequestConfig{BaseURL: u}
	for _, testCase := range attrTestData {
		out := bytes.NewBuffer(nil)
		sanitizeAttr(rc, out, testCase.AttrName, testCase.AttrValue, testCase.AttrValue)
		res, _ := out.ReadBytes(byte(0))
		if !bytes.Equal(res, testCase.ExpectedOutput) {
			t.Errorf(
				`Attribute parse error. Name: "%s", Value: "%s", Expected: %s, Got: "%s"`,
				testCase.AttrName,
				testCase.AttrValue,
				testCase.ExpectedOutput,
				res,
			)
		}
	}
}

func TestSanitizeURI(t *testing.T) {
	for _, testCase := range sanitizeUriTestData {
		newUrl, scheme := sanitizeURI(testCase.Input)
		if !bytes.Equal(newUrl, testCase.ExpectedOutput) {
			t.Errorf(
				`URL proxifier error. Expected: "%s", Got: "%s"`,
				testCase.ExpectedOutput,
				newUrl,
			)
		}
		if scheme != testCase.ExpectedScheme {
			t.Errorf(
				`URL proxifier error. Expected: "%s", Got: "%s"`,
				testCase.ExpectedScheme,
				scheme,
			)
		}
	}
}

func TestURLProxifier(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1/")
	rc := &RequestConfig{BaseURL: u}
	for _, testCase := range urlTestData {
		newUrl, err := rc.ProxifyURI([]byte(testCase.Input))
		if err != nil {
			t.Errorf("Failed to parse URL: %s", testCase.Input)
		}
		if newUrl != testCase.ExpectedOutput {
			t.Errorf(
				`URL proxifier error. Expected: "%s", Got: "%s"`,
				testCase.ExpectedOutput,
				newUrl,
			)
		}
	}
}

var BenchSimpleHtml = []byte(`<!doctype html>
<html>
 <head>
  <title>test</title>
 </head>
 <body>
  <h1>Test heading</h1>
 </body>
</html>`)

func BenchmarkSanitizeSimpleHTML(b *testing.B) {
	u, _ := url.Parse("http://127.0.0.1/")
	rc := &RequestConfig{BaseURL: u}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := bytes.NewBuffer(nil)
		sanitizeHTML(rc, out, BenchSimpleHtml)
	}
}

var BenchComplexHtml = []byte(`<!doctype html>
<html>
 <head>
  <noscript><meta http-equiv="refresh" content="0; URL=./xy"></noscript>
  <title>test 2</title>
  <script> alert('xy'); </script>
  <link rel="stylesheet" href="./core.bundle.css">
  <style>
   html { background: url(./a.jpg); }
  </style
 </head>
 <body>
  <h1>Test heading</h1>
  <img src="b.png" alt="imgtitle" />
  <form action="/z">
  <input type="submit" style="background: url(http://aa.bb/cc)" >
  </form>
 </body>
</html>`)

func BenchmarkSanitizeComplexHTML(b *testing.B) {
	u, _ := url.Parse("http://127.0.0.1/")
	rc := &RequestConfig{BaseURL: u}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := bytes.NewBuffer(nil)
		sanitizeHTML(rc, out, BenchComplexHtml)
	}
}
