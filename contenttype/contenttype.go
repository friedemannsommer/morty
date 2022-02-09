package contenttype

import (
	"mime"
	"strings"
)

type ContentType struct {
	TopLevelType string
	SubType      string
	Suffix       string
	Parameters   map[string]string
}

func (contentType *ContentType) String() string {
	var mimetype string
	if contentType.Suffix == "" {
		if contentType.SubType == "" {
			mimetype = contentType.TopLevelType
		} else {
			mimetype = contentType.TopLevelType + "/" + contentType.SubType
		}
	} else {
		mimetype = contentType.TopLevelType + "/" + contentType.SubType + "+" + contentType.Suffix
	}
	return mime.FormatMediaType(mimetype, contentType.Parameters)
}

func (contentType *ContentType) Equals(other ContentType) bool {
	if contentType.TopLevelType != other.TopLevelType ||
		contentType.SubType != other.SubType ||
		contentType.Suffix != other.Suffix ||
		len(contentType.Parameters) != len(other.Parameters) {
		return false
	}
	for k, v := range contentType.Parameters {
		if other.Parameters[k] != v {
			return false
		}
	}
	return true
}

func (contentType *ContentType) FilterParameters(parameters map[string]bool) {
	for k := range contentType.Parameters {
		if !parameters[k] {
			delete(contentType.Parameters, k)
		}
	}
}

func ParseContentType(contentType string) (ContentType, error) {
	mimetype, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ContentType{"", "", "", params}, err
	}
	splittedMimetype := strings.SplitN(strings.ToLower(mimetype), "/", 2)
	if len(splittedMimetype) <= 1 {
		return ContentType{splittedMimetype[0], "", "", params}, nil
	} else {
		splittedSubtype := strings.SplitN(splittedMimetype[1], "+", 2)
		if len(splittedSubtype) == 1 {
			return ContentType{splittedMimetype[0], splittedSubtype[0], "", params}, nil
		} else {
			return ContentType{splittedMimetype[0], splittedSubtype[0], splittedSubtype[1], params}, nil
		}
	}

}

type Filter func(contentType ContentType) bool

func NewFilterContains(partialMimeType string) Filter {
	return func(contentType ContentType) bool {
		return strings.Contains(contentType.TopLevelType, partialMimeType) ||
			strings.Contains(contentType.SubType, partialMimeType) ||
			strings.Contains(contentType.Suffix, partialMimeType)
	}
}

func NewFilterEquals(TopLevelType, SubType, Suffix string) Filter {
	return func(contentType ContentType) bool {
		return ((TopLevelType != "*" && TopLevelType == contentType.TopLevelType) || (TopLevelType == "*")) &&
			((SubType != "*" && SubType == contentType.SubType) || (SubType == "*")) &&
			((Suffix != "*" && Suffix == contentType.Suffix) || (Suffix == "*"))
	}
}

func NewFilterOr(contentTypeFilterList []Filter) Filter {
	return func(contentType ContentType) bool {
		for _, contentTypeFilter := range contentTypeFilterList {
			if contentTypeFilter(contentType) {
				return true
			}
		}
		return false
	}
}
