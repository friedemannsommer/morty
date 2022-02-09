package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"

	"github.com/friedemannsommer/morty/config"
	"github.com/friedemannsommer/morty/contenttype"
)

const (
	StateDefault    int = 0
	StateInStyle    int = 1
	StateInNoscript int = 2
)

const VERSION = "v0.2.1"

const MaxRedirectCount = 5

var CLIENT = &fasthttp.Client{
	MaxResponseBodySize: 10 * 1024 * 1024, // 10M
	ReadBufferSize:      16 * 1024,        // 16K
}

var cfg = config.DefaultConfig

var AllowedContentTypeFilter = contenttype.NewFilterOr([]contenttype.Filter{
	// html
	contenttype.NewFilterEquals("text", "html", ""),
	contenttype.NewFilterEquals("application", "xhtml", "xml"),
	// css
	contenttype.NewFilterEquals("text", "css", ""),
	// images
	contenttype.NewFilterEquals("image", "gif", ""),
	contenttype.NewFilterEquals("image", "png", ""),
	contenttype.NewFilterEquals("image", "jpeg", ""),
	contenttype.NewFilterEquals("image", "pjpeg", ""),
	contenttype.NewFilterEquals("image", "webp", ""),
	contenttype.NewFilterEquals("image", "tiff", ""),
	contenttype.NewFilterEquals("image", "vnd.microsoft.icon", ""),
	contenttype.NewFilterEquals("image", "bmp", ""),
	contenttype.NewFilterEquals("image", "x-ms-bmp", ""),
	contenttype.NewFilterEquals("image", "x-icon", ""),
	// fonts
	contenttype.NewFilterEquals("application", "font-otf", ""),
	contenttype.NewFilterEquals("application", "font-ttf", ""),
	contenttype.NewFilterEquals("application", "font-woff", ""),
	contenttype.NewFilterEquals("application", "vnd.ms-fontobject", ""),
})

var AllowedContentTypeAttachmentFilter = contenttype.NewFilterOr([]contenttype.Filter{
	// texts
	contenttype.NewFilterEquals("text", "csv", ""),
	contenttype.NewFilterEquals("text", "tab-separated-values", ""),
	contenttype.NewFilterEquals("text", "plain", ""),
	// API
	contenttype.NewFilterEquals("application", "json", ""),
	// Documents
	contenttype.NewFilterEquals("application", "x-latex", ""),
	contenttype.NewFilterEquals("application", "pdf", ""),
	contenttype.NewFilterEquals("application", "vnd.oasis.opendocument.text", ""),
	contenttype.NewFilterEquals("application", "vnd.oasis.opendocument.spreadsheet", ""),
	contenttype.NewFilterEquals("application", "vnd.oasis.opendocument.presentation", ""),
	contenttype.NewFilterEquals("application", "vnd.oasis.opendocument.graphics", ""),
	// Compressed archives
	contenttype.NewFilterEquals("application", "zip", ""),
	contenttype.NewFilterEquals("application", "gzip", ""),
	contenttype.NewFilterEquals("application", "x-compressed", ""),
	contenttype.NewFilterEquals("application", "x-gtar", ""),
	contenttype.NewFilterEquals("application", "x-compress", ""),
	// Generic binary
	contenttype.NewFilterEquals("application", "octet-stream", ""),
})

var AllowedContentTypeParameters = map[string]bool{
	"charset": true,
}

var UnsafeElements = [][]byte{
	[]byte("applet"),
	[]byte("canvas"),
	[]byte("embed"),
	[]byte("iframe"),
	[]byte("math"),
	[]byte("script"),
	[]byte("svg"),
}

var SafeAttributes = [][]byte{
	[]byte("abbr"),
	[]byte("accesskey"),
	[]byte("align"),
	[]byte("alt"),
	[]byte("as"),
	[]byte("autocomplete"),
	[]byte("charset"),
	[]byte("checked"),
	[]byte("class"),
	[]byte("content"),
	[]byte("contenteditable"),
	[]byte("contextmenu"),
	[]byte("dir"),
	[]byte("for"),
	[]byte("height"),
	[]byte("hidden"),
	[]byte("hreflang"),
	[]byte("id"),
	[]byte("lang"),
	[]byte("media"),
	[]byte("method"),
	[]byte("name"),
	[]byte("nowrap"),
	[]byte("placeholder"),
	[]byte("property"),
	[]byte("rel"),
	[]byte("spellcheck"),
	[]byte("tabindex"),
	[]byte("target"),
	[]byte("title"),
	[]byte("translate"),
	[]byte("type"),
	[]byte("value"),
	[]byte("width"),
}

var LinkRelSafeValues = [][]byte{
	[]byte("alternate"),
	[]byte("archives"),
	[]byte("author"),
	[]byte("copyright"),
	[]byte("first"),
	[]byte("help"),
	[]byte("icon"),
	[]byte("index"),
	[]byte("last"),
	[]byte("license"),
	[]byte("manifest"),
	[]byte("next"),
	// []byte("pingback"),
	[]byte("prev"),
	[]byte("publisher"),
	[]byte("search"),
	[]byte("shortcut icon"),
	[]byte("stylesheet"),
	[]byte("up"),
}

var LinkHttpEquivSafeValues = [][]byte{
	// X-UA-Compatible will be added automatically, so it can be skipped
	[]byte("date"),
	[]byte("last-modified"),
	[]byte("refresh"), // URL rewrite
	// []byte("location"), TODO URL rewrite
	[]byte("content-language"),
}

var CssUrlRegexp = regexp.MustCompile("url\\((['\"]?)[ \\t\\f]*([\u0009\u0021\u0023-\u0026\u0028\u002a-\u007E]+)(['\"]?)\\)?")

type Proxy struct {
	Key            []byte
	RequestTimeout time.Duration
	FollowRedirect bool
}

type RequestConfig struct {
	Key          []byte
	BaseURL      *url.URL
	BodyInjected bool
}

type HTMLBodyExtParam struct {
	BaseURL     string
	HasMortyKey bool
}

type HTMLFormExtParam struct {
	BaseURL   string
	MortyHash string
}

var HtmlFormExtension *template.Template
var HtmlBodyExtension *template.Template
var HtmlHeadContentType = `<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
<meta http-equiv="X-UA-Compatible" content="IE=edge">
<meta name="referrer" content="no-referrer">
`
var MortyHtmlPageStart = `<!doctype html>
<html>
<head>
<title>MortyProxy</title>
<meta name="viewport" content="width=device-width, initial-scale=1 , maximum-scale=1.0, user-scalable=1" />
<style>
html { height: 100%; }
body { min-height : 100%; display: flex; flex-direction:column; font-family: 'Garamond', 'Georgia', serif; text-align: center; color: #444; background: #FAFAFA; margin: 0; padding: 0; font-size: 1.1em; }
input { border: 1px solid #888; padding: 0.3em; color: #444; background: #FFF; font-size: 1.1em; }
input[placeholder] { width:80%; }
a { text-decoration: none; #2980b9; }
h1, h2 { font-weight: 200; margin-bottom: 2rem; }
h1 { font-size: 3em; }
.container { flex:1; min-height: 100%; margin-bottom: 1em; }
.footer { margin: 1em; }
.footer p { font-size: 0.8em; }
</style>
</head>
<body>
	<div class="container">
		<h1>MortyProxy</h1>
`
var MortyHtmlPageEnd = `
	</div>
	<div class="footer">
		<p>Morty rewrites web pages to exclude malicious HTML tags and CSS/HTML attributes. It also replaces external resource references to prevent third-party information leaks.<br />
		<a href="https://github.com/friedemannsommer/morty">view on github</a>
		</p>
	</div>
</body>
</html>`

var FaviconBytes []byte

func init() {
	FaviconBase64 := "iVBORw0KGgoAAAANSUhEUgAAABAAAAAQEAYAAABPYyMiAAAABmJLR0T///////8JWPfcAAAACXBIWXMAAABIAAAASABGyWs+AAAAF0lEQVRIx2NgGAWjYBSMglEwCkbBSAcACBAAAeaR9cIAAAAASUVORK5CYII"
	FaviconBytes, _ = base64.StdEncoding.DecodeString(FaviconBase64)

	var err error
	HtmlFormExtension, err = template.New("html_form_extension").Parse(
		`<input type="hidden" name="mortyurl" value="{{.BaseURL}}" />{{if .MortyHash}}<input type="hidden" name="mortyhash" value="{{.MortyHash}}" />{{end}}`)

	if err != nil {
		panic(err)
	}
	HtmlBodyExtension, err = template.New("html_body_extension").Parse(`
<input type="checkbox" id="mortytoggle" autocomplete="off" />
<div id="mortyheader">
  <form method="get">
    <label for="mortytoggle">hide</label>
    <span><a href="/">Morty Proxy</a></span>
    <input type="url" value="{{.BaseURL}}" name="mortyurl" {{if .HasMortyKey }}readonly="true"{{end}} />
    This is a <a href="https://github.com/friedemannsommer/morty">proxified and sanitized</a> view of the page, visit <a href="{{.BaseURL}}" rel="noreferrer">original site</a>.
  </form>
</div>
<style>
body{ position: absolute !important; top: 42px !important; left: 0 !important; right: 0 !important; bottom: 0 !important; }
#mortyheader { position: fixed; margin: 0; box-sizing: border-box; -webkit-box-sizing: border-box; top: 0; left: 0; right: 0; z-index: 2147483647 !important; font-size: 12px; line-height: normal; border-width: 0px 0px 2px 0; border-style: solid; border-color: #AAAAAA; background: #FFF; padding: 4px; color: #444; height: 42px; }
#mortyheader * { padding: 0; margin: 0; }
#mortyheader p { padding: 0 0 0.7em 0; display: block; }
#mortyheader a { color: #3498db; font-weight: bold; display: inline; }
#mortyheader label { text-align: right; cursor: pointer; position: fixed; right: 4px; top: 4px; display: block; color: #444; }
#mortyheader > form > span { font-size: 24px; font-weight: bold; margin-right: 20px; margin-left: 20px; }
input[type=checkbox]#mortytoggle { display: none; }
input[type=checkbox]#mortytoggle:checked ~ div { display: none; visibility: hidden; }
#mortyheader input[type=url] { width: 50%; padding: 4px; font-size: 16px; }
</style>
`)
	if err != nil {
		panic(err)
	}
}

func (p *Proxy) RequestHandler(ctx *fasthttp.RequestCtx) {

	if appRequestHandler(ctx) {
		return
	}

	requestHash := popRequestParam(ctx, []byte("mortyhash"))
	requestURI := popRequestParam(ctx, []byte("mortyurl"))

	if requestURI == nil {
		p.serveMainPage(ctx, 200, nil)
		return
	}

	if p.Key != nil {
		if !verifyRequestURI(requestURI, requestHash, p.Key) {
			// HTTP status code 403 : Forbidden
			p.serveMainPage(ctx, 403, errors.New(`invalid "mortyhash" parameter`))
			return
		}
	}

	requestURIQuery := ctx.QueryArgs().QueryString()
	if len(requestURIQuery) > 0 {
		if bytes.ContainsRune(requestURI, '?') {
			requestURI = append(requestURI, '&')
		} else {
			requestURI = append(requestURI, '?')
		}
		requestURI = append(requestURI, requestURIQuery...)
	}

	p.ProcessUri(ctx, string(requestURI), 0)
}

func (p *Proxy) ProcessUri(ctx *fasthttp.RequestCtx, requestURIStr string, redirectCount int) {
	parsedURI, err := url.Parse(requestURIStr)

	if err != nil {
		// HTTP status code 500 : Internal Server Error
		p.serveMainPage(ctx, 500, err)
		return
	}

	if parsedURI.Scheme == "" {
		requestURIStr = "https://" + requestURIStr
		parsedURI, err = url.Parse(requestURIStr)
		if err != nil {
			p.serveMainPage(ctx, 500, err)
			return
		}
	}

	// Serve an intermediate page for protocols other than HTTP(S)
	if (parsedURI.Scheme != "http" && parsedURI.Scheme != "https") || strings.HasSuffix(parsedURI.Host, ".onion") {
		p.serveExitMortyPage(ctx, parsedURI)
		return
	}

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetConnectionClose()

	if cfg.Debug {
		log.Println(string(ctx.Method()), requestURIStr)
	}

	req.SetRequestURI(requestURIStr)
	req.Header.SetUserAgentBytes([]byte("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:96.0) Gecko/20100101 Firefox/96.0"))

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.Header.SetMethodBytes(ctx.Method())
	if ctx.IsPost() || ctx.IsPut() {
		req.SetBody(ctx.PostBody())
	}

	err = CLIENT.DoTimeout(req, resp, p.RequestTimeout)

	if err != nil {
		if err == fasthttp.ErrTimeout {
			// HTTP status code 504 : Gateway Time-Out
			p.serveMainPage(ctx, 504, err)
		} else {
			// HTTP status code 500 : Internal Server Error
			p.serveMainPage(ctx, 500, err)
		}
		return
	}

	if resp.StatusCode() != 200 {
		switch resp.StatusCode() {
		case 301, 302, 303, 307, 308:
			loc := resp.Header.Peek("Location")
			if loc != nil {
				if p.FollowRedirect && ctx.IsGet() {
					// GET method: Morty follows the redirect
					if redirectCount < MaxRedirectCount {
						if cfg.Debug {
							log.Println("follow redirect to", string(loc))
						}
						p.ProcessUri(ctx, string(loc), redirectCount+1)
					} else {
						p.serveMainPage(ctx, 310, errors.New("too many redirects"))
					}
					return
				} else {
					// Other HTTP methods: Morty does NOT follow the redirect
					rc := &RequestConfig{Key: p.Key, BaseURL: parsedURI}
					proxyUri, err := rc.ProxifyURI(loc)
					if err == nil {
						ctx.SetStatusCode(resp.StatusCode())
						ctx.Response.Header.Add("Location", proxyUri)
						if cfg.Debug {
							log.Println("redirect to", string(loc))
						}
						return
					}
				}
			}
		}
		errorMessage := fmt.Sprintf("invalid response: %d (%s)", resp.StatusCode(), requestURIStr)
		p.serveMainPage(ctx, resp.StatusCode(), errors.New(errorMessage))
		return
	}

	contentTypeBytes := resp.Header.Peek("Content-Type")

	if contentTypeBytes == nil {
		// HTTP status code 503 : Service Unavailable
		p.serveMainPage(ctx, 503, errors.New("invalid content type"))
		return
	}

	contentTypeString := string(contentTypeBytes)

	// decode Content-Type header
	contentType, parseError := contenttype.ParseContentType(contentTypeString)
	if parseError != nil {
		// HTTP status code 503 : Service Unavailable
		p.serveMainPage(ctx, 503, errors.New("invalid content type"))
		return
	}

	// content-disposition
	contentDispositionBytes := ctx.Request.Header.Peek("Content-Disposition")

	// check content type
	if !AllowedContentTypeFilter(contentType) {
		// it is not a usual content type
		if AllowedContentTypeAttachmentFilter(contentType) {
			// force attachment for allowed content type
			contentDispositionBytes = contentDispositionForceAttachment(contentDispositionBytes, parsedURI)
		} else {
			// deny access to forbidden content type
			// HTTP status code 403 : Forbidden
			p.serveMainPage(ctx, 403, errors.New("forbidden content type "+parsedURI.String()))
			return
		}
	}

	// HACK : replace */xhtml by text/html
	if contentType.SubType == "xhtml" {
		contentType.TopLevelType = "text"
		contentType.SubType = "html"
		contentType.Suffix = ""
	}

	// conversion to UTF-8
	var responseBody []byte

	if contentType.TopLevelType == "text" {
		e, ename, _ := charset.DetermineEncoding(resp.Body(), contentTypeString)
		if (e != encoding.Nop) && (!strings.EqualFold("utf-8", ename)) {
			responseBody, err = e.NewDecoder().Bytes(resp.Body())
			if err != nil {
				// HTTP status code 503 : Service Unavailable
				p.serveMainPage(ctx, 503, err)
				return
			}
		} else {
			responseBody = resp.Body()
		}
		// update the charset or specify it
		contentType.Parameters["charset"] = "UTF-8"
	} else {
		responseBody = resp.Body()
	}

	//
	contentType.FilterParameters(AllowedContentTypeParameters)

	// set the content type
	ctx.SetContentType(contentType.String())

	// output according to MIME type
	switch {
	case contentType.SubType == "css" && contentType.Suffix == "":
		sanitizeCSS(&RequestConfig{Key: p.Key, BaseURL: parsedURI}, ctx, responseBody)
	case contentType.SubType == "html" && contentType.Suffix == "":
		rc := &RequestConfig{Key: p.Key, BaseURL: parsedURI}
		sanitizeHTML(rc, ctx, responseBody)
		if !rc.BodyInjected {
			p := HTMLBodyExtParam{rc.BaseURL.String(), false}
			if len(rc.Key) > 0 {
				p.HasMortyKey = true
			}
			err := HtmlBodyExtension.Execute(ctx, p)
			if err != nil {
				if cfg.Debug {
					fmt.Println("failed to inject body extension", err)
				}
			}
		}
	default:
		if contentDispositionBytes != nil {
			ctx.Response.Header.AddBytesV("Content-Disposition", contentDispositionBytes)
		}
		_, _ = ctx.Write(responseBody)
	}
}

// force content-disposition to attachment
func contentDispositionForceAttachment(contentDispositionBytes []byte, url *url.URL) []byte {
	var contentDispositionParams map[string]string

	if contentDispositionBytes != nil {
		var err error
		_, contentDispositionParams, err = mime.ParseMediaType(string(contentDispositionBytes))
		if err != nil {
			contentDispositionParams = make(map[string]string)
		}
	} else {
		contentDispositionParams = make(map[string]string)
	}

	_, fileNameDefined := contentDispositionParams["filename"]
	if !fileNameDefined {
		// TODO : sanitize filename
		contentDispositionParams["fileName"] = filepath.Base(url.Path)
	}

	return []byte(mime.FormatMediaType("attachment", contentDispositionParams))
}

func appRequestHandler(ctx *fasthttp.RequestCtx) bool {
	// serve robots.txt
	if bytes.Equal(ctx.Path(), []byte("/robots.txt")) {
		ctx.SetContentType("text/plain")
		_, _ = ctx.Write([]byte("User-Agent: *\nDisallow: /\n"))
		return true
	}

	// server favicon.ico
	if bytes.Equal(ctx.Path(), []byte("/favicon.ico")) {
		ctx.SetContentType("image/png")
		_, _ = ctx.Write(FaviconBytes)
		return true
	}

	return false
}

func popRequestParam(ctx *fasthttp.RequestCtx, paramName []byte) []byte {
	param := ctx.QueryArgs().PeekBytes(paramName)

	if param == nil {
		param = ctx.PostArgs().PeekBytes(paramName)
		ctx.PostArgs().DelBytes(paramName)
	}
	ctx.QueryArgs().DelBytes(paramName)

	return param
}

func sanitizeCSS(rc *RequestConfig, out io.Writer, css []byte) {
	urlSlices := CssUrlRegexp.FindAllSubmatchIndex(css, -1)

	if urlSlices == nil {
		_, _ = out.Write(css)
		return
	}

	startIndex := 0

	for _, s := range urlSlices {
		urlStart := s[4]
		urlEnd := s[5]

		if uri, err := rc.ProxifyURI(css[urlStart:urlEnd]); err == nil {
			_, _ = out.Write(css[startIndex:urlStart])
			_, _ = out.Write([]byte(uri))
			startIndex = urlEnd
		} else if cfg.Debug {
			log.Println("cannot proxify css uri:", string(css[urlStart:urlEnd]))
		}
	}
	if startIndex < len(css) {
		_, _ = out.Write(css[startIndex:])
	}
}

func sanitizeHTML(rc *RequestConfig, out io.Writer, htmlDoc []byte) {
	r := bytes.NewReader(htmlDoc)
	decoder := html.NewTokenizer(r)
	decoder.AllowCDATA(true)

	unsafeElements := make([][]byte, 0, 8)
	state := StateDefault
	for {
		token := decoder.Next()
		if token == html.ErrorToken {
			err := decoder.Err()
			if err != io.EOF {
				log.Println("failed to parse HTML")
			}
			break
		}

		if len(unsafeElements) == 0 {

			switch token {
			case html.StartTagToken, html.SelfClosingTagToken:
				tag, hasAttrs := decoder.TagName()
				safe := !inArray(tag, UnsafeElements)
				if !safe {
					if token != html.SelfClosingTagToken {
						var unsafeTag = make([]byte, len(tag))
						copy(unsafeTag, tag)
						unsafeElements = append(unsafeElements, unsafeTag)
					}
					break
				}
				if bytes.Equal(tag, []byte("base")) {
					for {
						attrName, attrValue, moreAttr := decoder.TagAttr()
						if bytes.Equal(attrName, []byte("href")) {
							parsedURI, err := url.Parse(string(attrValue))
							if err == nil {
								rc.BaseURL = parsedURI
							}
						}
						if !moreAttr {
							break
						}
					}
					break
				}
				if bytes.Equal(tag, []byte("noscript")) {
					state = StateInNoscript
					break
				}
				var attrs [][][]byte
				if hasAttrs {
					for {
						attrName, attrValue, moreAttr := decoder.TagAttr()
						attrs = append(attrs, [][]byte{
							attrName,
							attrValue,
							[]byte(html.EscapeString(string(attrValue))),
						})
						if !moreAttr {
							break
						}
					}
				}
				if bytes.Equal(tag, []byte("link")) {
					sanitizeLinkTag(rc, out, attrs)
					break
				}

				if bytes.Equal(tag, []byte("meta")) {
					sanitizeMetaTag(rc, out, attrs)
					break
				}

				_, _ = fmt.Fprintf(out, "<%s", tag)

				if hasAttrs {
					sanitizeAttrs(rc, out, attrs)
				}

				if token == html.SelfClosingTagToken {
					_, _ = fmt.Fprintf(out, " />")
				} else {
					_, _ = fmt.Fprintf(out, ">")
					if bytes.Equal(tag, []byte("style")) {
						state = StateInStyle
					}
				}

				if bytes.Equal(tag, []byte("head")) {
					_, _ = fmt.Fprintf(out, HtmlHeadContentType)
				}

				if bytes.Equal(tag, []byte("form")) {
					var formURL *url.URL
					for _, attr := range attrs {
						if bytes.Equal(attr[0], []byte("action")) {
							formURL, _ = url.Parse(string(attr[1]))
							formURL = mergeURIs(rc.BaseURL, formURL)
							break
						}
					}
					if formURL == nil {
						formURL = rc.BaseURL
					}
					urlStr := formURL.String()
					var key string
					if rc.Key != nil {
						key = hash(urlStr, rc.Key)
					}
					err := HtmlFormExtension.Execute(out, HTMLFormExtParam{urlStr, key})
					if err != nil {
						if cfg.Debug {
							fmt.Println("failed to inject body extension", err)
						}
					}
				}

			case html.EndTagToken:
				tag, _ := decoder.TagName()
				writeEndTag := true
				switch string(tag) {
				case "body":
					p := HTMLBodyExtParam{rc.BaseURL.String(), false}
					if len(rc.Key) > 0 {
						p.HasMortyKey = true
					}
					err := HtmlBodyExtension.Execute(out, p)
					if err != nil {
						if cfg.Debug {
							fmt.Println("failed to inject body extension", err)
						}
					}
					rc.BodyInjected = true
				case "style":
					state = StateDefault
				case "noscript":
					state = StateDefault
					writeEndTag = false
				}
				// skip noscript tags - only the tag, not the content, because javascript is sanitized
				if writeEndTag {
					_, _ = fmt.Fprintf(out, "</%s>", tag)
				}

			case html.TextToken:
				switch state {
				case StateDefault:
					_, _ = fmt.Fprintf(out, "%s", decoder.Raw())
				case StateInStyle:
					sanitizeCSS(rc, out, decoder.Raw())
				case StateInNoscript:
					sanitizeHTML(rc, out, decoder.Raw())
				}

			case html.CommentToken:
				// ignore comment. TODO : parse IE conditional comment
			case html.DoctypeToken:
				_, _ = out.Write(decoder.Raw())
			}
		} else {
			switch token {
			case html.StartTagToken, html.SelfClosingTagToken:
				tag, _ := decoder.TagName()
				if inArray(tag, UnsafeElements) {
					unsafeElements = append(unsafeElements, tag)
				}

			case html.EndTagToken:
				tag, _ := decoder.TagName()
				if bytes.Equal(unsafeElements[len(unsafeElements)-1], tag) {
					unsafeElements = unsafeElements[:len(unsafeElements)-1]
				}
			}
		}
	}
}

func sanitizeLinkTag(rc *RequestConfig, out io.Writer, attrs [][][]byte) {
	exclude := false
	for _, attr := range attrs {
		attrName := attr[0]
		attrValue := attr[1]
		if bytes.Equal(attrName, []byte("rel")) {
			if !inArray(attrValue, LinkRelSafeValues) {
				exclude = true
				break
			}
		}
		if bytes.Equal(attrName, []byte("as")) {
			if bytes.Equal(attrValue, []byte("script")) {
				exclude = true
				break
			}
		}
	}

	if !exclude {
		_, _ = out.Write([]byte("<link"))
		for _, attr := range attrs {
			sanitizeAttr(rc, out, attr[0], attr[1], attr[2])
		}
		_, _ = out.Write([]byte(">"))
	}
}

func sanitizeMetaTag(rc *RequestConfig, out io.Writer, attrs [][][]byte) {
	var httpEquiv []byte
	var content []byte

	for _, attr := range attrs {
		attrName := attr[0]
		attrValue := attr[1]
		if bytes.Equal(attrName, []byte("http-equiv")) {
			httpEquiv = bytes.ToLower(attrValue)
			// exclude some <meta http-equiv="..." ..>
			if !inArray(httpEquiv, LinkHttpEquivSafeValues) {
				return
			}
		}
		if bytes.Equal(attrName, []byte("content")) {
			content = attrValue
		}
		if bytes.Equal(attrName, []byte("charset")) {
			// exclude <meta charset="...">
			return
		}
	}

	_, _ = out.Write([]byte("<meta"))
	urlIndex := bytes.Index(bytes.ToLower(content), []byte("url="))
	if bytes.Equal(httpEquiv, []byte("refresh")) && urlIndex != -1 {
		contentUrl := content[urlIndex+4:]
		// special case of <meta http-equiv="refresh" content="0; url='example.com/url.with.quote.outside'">
		if len(contentUrl) >= 2 && (contentUrl[0] == byte('\'') || contentUrl[0] == byte('"')) {
			if contentUrl[0] == contentUrl[len(contentUrl)-1] {
				contentUrl = contentUrl[1 : len(contentUrl)-1]
			}
		}
		// output proxify result
		if uri, err := rc.ProxifyURI(contentUrl); err == nil {
			_, _ = fmt.Fprintf(out, ` http-equiv="refresh" content="%surl=%s"`, content[:urlIndex], uri)
		}
	} else {
		if len(httpEquiv) > 0 {
			_, _ = fmt.Fprintf(out, ` http-equiv="%s"`, httpEquiv)
		}
		sanitizeAttrs(rc, out, attrs)
	}
	_, _ = out.Write([]byte(">"))
}

func sanitizeAttrs(rc *RequestConfig, out io.Writer, attrs [][][]byte) {
	for _, attr := range attrs {
		sanitizeAttr(rc, out, attr[0], attr[1], attr[2])
	}
}

func sanitizeAttr(rc *RequestConfig, out io.Writer, attrName, attrValue, escapedAttrValue []byte) {
	if inArray(attrName, SafeAttributes) {
		_, _ = fmt.Fprintf(out, " %s=\"%s\"", attrName, escapedAttrValue)
		return
	}
	switch string(attrName) {
	case "src", "href", "action":
		if uri, err := rc.ProxifyURI(attrValue); err == nil {
			_, _ = fmt.Fprintf(out, " %s=\"%s\"", attrName, uri)
		} else if cfg.Debug {
			log.Println("cannot proxify uri:", string(attrValue))
		}
	case "style":
		cssAttr := bytes.NewBuffer(nil)
		sanitizeCSS(rc, cssAttr, attrValue)
		_, _ = fmt.Fprintf(out, " %s=\"%s\"", attrName, html.EscapeString(string(cssAttr.Bytes())))
	}
}

func mergeURIs(u1, u2 *url.URL) *url.URL {
	if u2 == nil {
		return u1
	}
	return u1.ResolveReference(u2)
}

// Sanitized URI : removes all runes bellow 32 (included) as the beginning and end of URI, and lower case the scheme.
// avoid memory allocation (except for the scheme)
func sanitizeURI(uri []byte) ([]byte, string) {
	firstRuneIndex := 0
	firstRuneSeen := false
	schemeLastIndex := -1
	buffer := bytes.NewBuffer(make([]byte, 0, 10))

	// remove trailing space and special characters
	uri = bytes.TrimRight(uri, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0A\x0B\x0C\x0D\x0E\x0F\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1A\x1B\x1C\x1D\x1E\x1F\x20")

	// loop over byte by byte
	for i, c := range uri {
		// ignore special characters and space (c <= 32)
		if c > 32 {
			// append to the lower case of the rune to buffer
			if c < utf8.RuneSelf && 'A' <= c && c <= 'Z' {
				c = c + 'a' - 'A'
			}

			buffer.WriteByte(c)

			// update the first rune index that is not a special rune
			if !firstRuneSeen {
				firstRuneIndex = i
				firstRuneSeen = true
			}

			if c == ':' {
				// colon rune found, we have found the scheme
				schemeLastIndex = i
				break
			} else if c == '/' || c == '?' || c == '\\' || c == '#' {
				// special case : most probably a relative URI
				break
			}
		}
	}

	if schemeLastIndex != -1 {
		// scheme found
		// copy the "lower case without special runes scheme" before the ":" rune
		schemeStartIndex := schemeLastIndex - buffer.Len() + 1
		copy(uri[schemeStartIndex:], buffer.Bytes())
		// and return the result
		return uri[schemeStartIndex:], buffer.String()
	} else {
		// scheme NOT found
		return uri[firstRuneIndex:], ""
	}
}

func (rc *RequestConfig) ProxifyURI(uri []byte) (string, error) {
	// sanitize URI
	uri, scheme := sanitizeURI(uri)

	// remove javascript protocol
	if scheme == "javascript:" {
		return "", nil
	}

	// TODO check malicious data: - e.g. data:script
	if scheme == "data:" {
		if bytes.HasPrefix(uri, []byte("data:image/png")) ||
			bytes.HasPrefix(uri, []byte("data:image/jpeg")) ||
			bytes.HasPrefix(uri, []byte("data:image/pjpeg")) ||
			bytes.HasPrefix(uri, []byte("data:image/gif")) ||
			bytes.HasPrefix(uri, []byte("data:image/webp")) {
			// should be safe
			return string(uri), nil
		} else {
			// unsafe data
			return "", nil
		}
	}

	// parse the uri
	u, err := url.Parse(string(uri))
	if err != nil {
		return "", err
	}

	// get the fragment (with the prefix "#")
	fragment := ""
	if len(u.Fragment) > 0 {
		fragment = "#" + u.Fragment
	}

	// reset the fragment: it is not included in the mortyurl
	u.Fragment = ""

	// merge the URI with the document URI
	u = mergeURIs(rc.BaseURL, u)

	// simple internal link ?
	// some web pages describe the whole link https://same:auth@same.host/same.path?same.query#new.fragment
	if u.Scheme == rc.BaseURL.Scheme &&
		(rc.BaseURL.User == nil || (u.User != nil && u.User.String() == rc.BaseURL.User.String())) &&
		u.Host == rc.BaseURL.Host &&
		u.Path == rc.BaseURL.Path &&
		u.RawQuery == rc.BaseURL.RawQuery {
		// the fragment is the only difference between the document URI and the uri parameter
		return fragment, nil
	}

	// return full URI and fragment (if not empty)
	mortyUri := u.String()

	if rc.Key == nil {
		return fmt.Sprintf("./?mortyurl=%s%s", url.QueryEscape(mortyUri), fragment), nil
	}
	return fmt.Sprintf("./?mortyhash=%s&mortyurl=%s%s", hash(mortyUri, rc.Key), url.QueryEscape(mortyUri), fragment), nil
}

func inArray(b []byte, a [][]byte) bool {
	for _, b2 := range a {
		if bytes.Equal(b, b2) {
			return true
		}
	}
	return false
}

func hash(msg string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyRequestURI(uri, hashMsg, key []byte) bool {
	h := make([]byte, hex.DecodedLen(len(hashMsg)))
	_, err := hex.Decode(h, hashMsg)
	if err != nil {
		if cfg.Debug {
			log.Println("hmac error:", err)
		}
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(uri)
	return hmac.Equal(h, mac.Sum(nil))
}

func (p *Proxy) serveExitMortyPage(ctx *fasthttp.RequestCtx, uri *url.URL) {
	ctx.SetContentType("text/html")
	ctx.SetStatusCode(403)
	_, _ = ctx.Write([]byte(MortyHtmlPageStart))
	_, _ = ctx.Write([]byte("<h2>You are about to exit MortyProxy</h2>"))
	_, _ = ctx.Write([]byte("<p>Following</p><p><a href=\""))
	_, _ = ctx.Write([]byte(html.EscapeString(uri.String())))
	_, _ = ctx.Write([]byte("\" rel=\"noreferrer\">"))
	_, _ = ctx.Write([]byte(html.EscapeString(uri.String())))
	_, _ = ctx.Write([]byte("</a></p><p>the content of this URL will be <b>NOT</b> sanitized.</p>"))
	_, _ = ctx.Write([]byte(MortyHtmlPageEnd))
}

func (p *Proxy) serveMainPage(ctx *fasthttp.RequestCtx, statusCode int, err error) {
	ctx.SetContentType("text/html; charset=UTF-8")
	ctx.SetStatusCode(statusCode)
	_, _ = ctx.Write([]byte(MortyHtmlPageStart))
	if err != nil {
		if cfg.Debug {
			log.Println("error:", err)
		}
		_, _ = ctx.Write([]byte("<h2>Error: "))
		_, _ = ctx.Write([]byte(html.EscapeString(err.Error())))
		_, _ = ctx.Write([]byte("</h2>"))
	}
	if p.Key == nil {
		_, _ = ctx.Write([]byte(`
		<form action="post">
		Visit url: <input placeholder="https://url.." name="mortyurl" autofocus />
		<input type="submit" value="go" />
		</form>`))
	} else {
		_, _ = ctx.Write([]byte(`<h3>Warning! This instance does not support direct URL opening.</h3>`))
	}
	_, _ = ctx.Write([]byte(MortyHtmlPageEnd))
}

func main() {
	var hmacKey string

	flag.StringVar(&hmacKey, "key", "", "HMAC url validation key (base64 encoded) - leave blank to disable validation")
	listenAddress := flag.String("listen", cfg.ListenAddress, "Listen address")
	IPV6 := flag.Bool("ipv6", cfg.IPV6, "Allow IPv6 HTTP requests")
	debug := flag.Bool("debug", cfg.Debug, "Debug mode")
	requestTimeoutStr := flag.String("timeout", "", "Request timeout")
	followRedirect := flag.Bool("followredirect", cfg.FollowRedirect, "Follow HTTP GET redirect")
	proxyEnv := flag.Bool("proxyenv", false, "Use a HTTP proxy as set in the environment (HTTP_PROXY, HTTPS_PROXY and NO_PROXY). Overrides -proxy, -socks5, -ipv6.")
	proxy := flag.String("proxy", "", "Use the specified HTTP proxy (ie: '[user:pass@]hostname:port'). Overrides -socks5, -ipv6.")
	socks5 := flag.String("socks5", "", "Use a SOCKS5 proxy (ie: 'hostname:port'). Overrides -ipv6.")
	version := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *version {
		fmt.Println(VERSION)
		return
	}

	cfg.ListenAddress = *listenAddress
	cfg.IPV6 = *IPV6
	cfg.Debug = *debug
	cfg.FollowRedirect = *followRedirect

	if *proxyEnv && os.Getenv("HTTP_PROXY") == "" && os.Getenv("HTTPS_PROXY") == "" {
		log.Fatal("Error -proxyenv is used but no environment variables named 'HTTP_PROXY' and/or 'HTTPS_PROXY' could be found.")
	}

	if cfg.ListenAddress == "" {
		log.Fatal("Error no listen address defined")
	}

	if hmacKey == "" {
		hmacKey = os.Getenv("MORTY_KEY")
	}

	if *requestTimeoutStr != "" {
		parsedUint, err := strconv.ParseUint(*requestTimeoutStr, 10, 8)

		if err != nil {
			log.Fatalf("Error -timeout is to large: %v", err)
		}

		cfg.RequestTimeout = uint8(parsedUint)
	}

	cfg.Key = hmacKey

	if cfg.Debug {
		fmt.Printf("Using config: %+v\n", cfg)
	}

	if *proxyEnv {
		CLIENT.Dial = fasthttpproxy.FasthttpProxyHTTPDialer()
		log.Println("Using environment defined proxy(ies).")
	} else if *proxy != "" {
		CLIENT.Dial = fasthttpproxy.FasthttpHTTPDialer(*proxy)
		log.Println("Using custom HTTP proxy.")
	} else if *socks5 != "" {
		CLIENT.Dial = fasthttpproxy.FasthttpSocksDialer(*socks5)
		log.Println("Using Socks5 proxy.")
	} else if cfg.IPV6 {
		CLIENT.Dial = fasthttp.DialDualStack
		log.Println("Using dual stack (IPv4/IPv6) direct connections.")
	} else {
		CLIENT.Dial = fasthttp.Dial
		log.Println("Using IPv4 only direct connections.")
	}

	p := &Proxy{RequestTimeout: time.Duration(cfg.RequestTimeout) * time.Second,
		FollowRedirect: cfg.FollowRedirect}

	if cfg.Key != "" {
		var err error

		p.Key, err = base64.StdEncoding.DecodeString(cfg.Key)

		if err != nil {
			log.Fatalf("Error parsing -key: %v", err.Error())
		}
	}

	log.Println("listening on:", cfg.ListenAddress)

	if err := fasthttp.ListenAndServe(cfg.ListenAddress, p.RequestHandler); err != nil {
		log.Fatalf("Error in ListenAndServe: %v", err)
	}
}
