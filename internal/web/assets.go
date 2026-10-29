package web

import (
	"net/url"
	"path"
)

type AssetHelper struct {
	baseURL *url.URL
	prefix  string
	suffix  string
	query   map[string]string
}

func NewAssetHelper(baseURL, prefix, suffix string, query map[string]string) *AssetHelper {
	u, err := url.Parse(baseURL)

	if err != nil {
		panic("invalid base url: " + baseURL)
	}

	return &AssetHelper{
		baseURL: u,
		prefix:  prefix,
		suffix:  suffix,
		query:   query,
	}
}

func (a *AssetHelper) GetURL(location string) string {
	u, err := url.Parse(location)

	if err != nil {
		return location
	}

	u.Scheme = a.baseURL.Scheme
	u.Host = a.baseURL.Host
	u.Path = path.Join(a.prefix, u.Path, a.suffix)

	q := u.Query()

	for k, v := range a.query {
		q.Set(k, v)
	}

	// rebuild query
	u.RawQuery = q.Encode()

	return u.String()
}
