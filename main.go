package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-http-utils/logger"
	"github.com/gregjones/httpcache"
	"sourcegraph.com/sourcegraph/s3cache"
)

func main() {
	port := os.Getenv("PORT")

	if port == "" {
		port = "8989"
	}

	target, err := url.Parse("https://eodhistoricaldata.com")
	if err != nil {
		log.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	var cache httpcache.Cache
	cache = httpcache.NewMemoryCache()
	if os.Getenv("AWS_BUCKET") != "" {
		cache = s3cache.New(os.Getenv("AWS_BUCKET"))
	}

	transport := httpcache.NewTransport(cache)
	transport.Transport = NewCacheHeadersTransport()
	proxy.Transport = transport

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(res http.ResponseWriter, req *http.Request) {
		req.Host = req.URL.Host
		proxy.ServeHTTP(res, req)
	})

	err = http.ListenAndServe(":"+port, logger.Handler(mux, os.Stdout, logger.CommonLoggerType))
	if err != nil {
		panic(err)
	}
}

func NewCacheHeadersTransport() *CacheHeadersTransport {
	return &CacheHeadersTransport{
		transport: http.DefaultTransport,
	}
}

type CacheHeadersTransport struct {
	transport http.RoundTripper
}

func (e *CacheHeadersTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	resp, err = e.transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode == 200 {
		info := overWriteCacheControl(req, resp)
		log.Printf("%40s: %20s - %s\n", req.URL.Path, info, resp.Header.Get("cache-control"))
	}
	return resp, nil
}

func overWriteCacheControl(req *http.Request, resp *http.Response) string {
	if cacheByDate(req, resp) {
		return "cache by date"
	}
	if cacheMultipleDays(req, resp) {
		return "cache multi day"
	}
	if cacheSingleDate(req, resp) {
		return "cache single day"
	}
	return "no cache"
}

var singleDay = []string{"/exchanges/"}
var multiDay = []string{"/bulk-fundamentals/", "/fundamentals/"}
var olderDates = []string{"/eod-bulk-last-day/", "/eod-bulk-last-day/", "/eod/", "/div/", "/splits/"}

func cacheMultipleDays(req *http.Request, resp *http.Response) bool {
	if matchesPrefixes(multiDay, req) {
		resp.Header.Set("cache-control", "private, max-age=1296000") // set max age to 15 days
		return true
	}
	return false
}

func cacheByDate(req *http.Request, resp *http.Response) bool {
	if matchesPrefixes(olderDates, req) {
		dateString := req.URL.Query().Get("date")
		if dateString != "" {
			date, err := time.Parse("2006-01-02", dateString)
			if err == nil {
				if date.Before(time.Now().Add(-30 * time.Hour)) {
					resp.Header.Set("cache-control", "private, max-age=864000") // set max age to 10 days if date is before 30 days ago
					return true
				}
			}
		}
		resp.Header.Set("cache-control", "private, max-age=3600") // set max age to 1 hour
		return true
	}
	return false
}

func cacheSingleDate(req *http.Request, resp *http.Response) bool {
	if matchesPrefixes(singleDay, req) {
		resp.Header.Set("cache-control", "private, max-age=86400") // set max age to 15 days
		return true
	}
	return false

}
func matchesPrefixes(prefixes []string, req *http.Request) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(req.URL.Path, "/api"+prefix) {
			return true
		}
	}
	return false
}
