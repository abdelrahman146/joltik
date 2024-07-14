package crawler

import (
	"fmt"
	"github.com/gocolly/colly/v2"
	"log"
	"strconv"
	"time"
)

func NewCrawler(name string, pattern string) (*colly.Collector, error) {
	c := colly.NewCollector(
		// Cache responses to prevent multiple download of pages
		// even if the collector is restarted
		colly.CacheDir(fmt.Sprintf("./cache/%s", name)),
		// Set a User-Agent to mimic a real browser
		colly.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"),
		// Attach a debugger to the collector
		//colly.Debugger(&debug.LogDebugger{}),
	)
	// Set request timeout
	c.SetRequestTimeout(30 * time.Second)

	c.OnRequest(func(r *colly.Request) {
		r.Ctx.Put("retryCount", "0")
	})

	c.OnError(func(r *colly.Response, err error) {
		retryCount, _ := strconv.Atoi(r.Ctx.Get("retryCount"))
		if retryCount < 3 {
			log.Printf("Retry %d for %s\n", retryCount+1, r.Request.URL)
			r.Request.Ctx.Put("retryCount", strconv.Itoa(retryCount+1))
			time.Sleep(5 * time.Second) // Wait before retrying
			_ = r.Request.Retry()
		} else {
			log.Printf("Failed to visit %s after 3 retries: %v\n", r.Request.URL, err)
		}
	})

	// Limit the number of threads started by colly to 3
	// This is a good practice to avoid flooding a website with requests
	// and reduce the likelihood of the web scraping script from being banned
	// random delay between 1s and 25s
	if err := c.Limit(&colly.LimitRule{
		DomainGlob:  pattern,
		Parallelism: 3,
		Delay:       1 * time.Second,
		RandomDelay: 15 * time.Second,
	}); err != nil {
		log.Panic("Failed to limit the number of threads", err)
	}
	return c, nil
}
