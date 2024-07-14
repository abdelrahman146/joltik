package main

import (
	"fmt"
	"github.com/abdelrahman146/joltik/pkg/crawler"
	"github.com/gocolly/colly/v2"
	"github.com/reugn/go-streams/extension"
	"github.com/reugn/go-streams/flow"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Product struct {
	Category            string
	Website             string
	Title               string
	SellingPrice        float64
	PriceBeforeDiscount *float64
	Currency            string
	ProductURL          string
	SKU                 string
	Brand               string
	RatingScore         *float64
	RatingCount         *int
	Rank                int
	IsBestSeller        bool
}

func extractPrice(text string) (float64, error) {
	re := regexp.MustCompile(`\bAED\s+([\d,]+\.\d{2})\b`)
	cleanText := strings.ReplaceAll(text, "\u00A0", " ")
	match := re.FindStringSubmatch(cleanText)

	if len(match) == 0 {
		return 0, fmt.Errorf("price not found")
	}
	priceText := match[1]
	price, err := strconv.ParseFloat(strings.ReplaceAll(priceText, ",", ""), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price: %w", err)
	}
	return price, nil
}

func extractCategory(text string) string {
	cleanText := strings.ReplaceAll(text, "\u00A0", " ")
	// Define a regular expression to find the pattern where a lowercase letter is followed by an uppercase letter
	re := regexp.MustCompile(`([a-z])([A-Z])`)
	// Replace the pattern with the lowercase letter followed by a comma and the uppercase letter
	result := re.ReplaceAllString(cleanText, `$1,$2`)
	categories := strings.Split(result, ",")
	return categories[len(categories)-1]
}

func extractModelNumber(text string) (string, error) {
	re := regexp.MustCompile(`\bModel\s+Number\s*:\s*(\w+)\b`)
	cleanText := strings.ReplaceAll(text, "\u00A0", " ")
	match := re.FindStringSubmatch(cleanText)

	if len(match) == 0 {
		return "", fmt.Errorf("model number not found")
	}
	return match[1], nil
}

func (p Product) ToCSVRow() string {
	priceBeforeDiscount := ""
	if p.PriceBeforeDiscount != nil {
		priceBeforeDiscount = fmt.Sprintf("%.2f", *p.PriceBeforeDiscount)
	}
	ratingScore := ""
	if p.RatingScore != nil {
		ratingScore = fmt.Sprintf("%.2f", *p.RatingScore)
	}
	ratingCount := ""
	if p.RatingCount != nil {
		ratingCount = strconv.Itoa(*p.RatingCount)
	}
	return fmt.Sprintf("%s,%s,%s,%.2f,%s,%s,%s,%s,%s,%s,%d\n",
		p.Category,
		p.Website,
		p.Title,
		p.SellingPrice,
		priceBeforeDiscount,
		p.Currency,
		p.ProductURL,
		p.Brand,
		ratingScore,
		ratingCount,
		p.Rank)
}

var totalProducts = 0

func main() {
	file, err := os.OpenFile("products.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("failed to open file: %s", err)
	}

	_, err = file.WriteString("Category,Website,Title,SellingPrice,PriceBeforeDiscount,Currency,ProductURL,Brand,RatingScore,RatingCount,Rank\n")
	if err != nil {
		log.Panic("Failed to write to file ", err)
	}

	_ = file.Close()

	root := "https://www.noon.com"
	category := "/uae-en/beauty/personal-care-16343/men-grooming/?f%5BisCarousel%5D%5B%5D=true&limit=50&sort%5Bby%5D=popularity&sort%5Bdir%5D=desc"
	ch := make(chan any)
	wg := sync.WaitGroup{}

	plpCrawler, err := crawler.NewCrawler("noon", "*noon.*")
	if err != nil {
		log.Fatal("Failed to setup crawler ", err)
	}
	pdpCrawler, err := crawler.NewCrawler("noon", "*noon.*")
	if err != nil {
		log.Fatal("Failed to setup crawler ", err)
	}

	plpCrawler.OnHTML(".productContainer", func(e *colly.HTMLElement) {
		href := e.ChildAttr("a", "href")
		go func() {
			wg.Add(1)
			link := root + href
			if err := pdpCrawler.Visit(link); err != nil {
				log.Println("Failed to visit product page ", err)
			}
		}()
	})

	pdpCrawler.OnHTML("div[id=__next]", func(e *colly.HTMLElement) {
		defer wg.Done()
		bestSellerLink := e.ChildText(".bestSellerLink")
		productRaw := map[string]interface{}{
			"title":               e.ChildText("h1"),
			"categories":          e.ChildText("div[data-qa=breadcrumbs-list]"),
			"sellingPrice":        e.ChildText(".priceNow"),
			"priceBeforeDiscount": e.ChildText(".priceWas"),
			"productURL":          e.Request.URL.String(),
			"brand":               e.ChildText("div[data-qa^=pdp-brand-]"),
			"SKU":                 e.ChildText(".modelNumber"),
			"rating":              e.ChildText(".isPdp"),
			"isBestSeller":        bestSellerLink != "",
		}
		ch <- productRaw
	})

	for i := 1; i <= 5; i++ {
		link := root + category + "&page=" + strconv.Itoa(i)
		if err := plpCrawler.Visit(link); err != nil {
			log.Println("Failed to visit product list page ", i, err)
		}
	}

	go func() {
		pdpCrawler.Wait()
		wg.Wait()
		close(ch)
	}()

	source := extension.NewChanSource(ch)
	sink := extension.NewFileSink("products.csv")
	source.Via(flow.NewFilter[map[string]interface{}](func(item map[string]interface{}) bool {
		isBestSeller := item["isBestSeller"].(bool)
		var ratingCount int
		if item["rating"] != "" {
			if count, err := strconv.Atoi(item["rating"].(string)[3:]); err == nil {
				ratingCount = count
			}
		}
		if isBestSeller || ratingCount > 100 {
			return true
		}
		return false
	}, 1)).Via(flow.NewMap[map[string]interface{}, Product](func(item map[string]interface{}) Product {
		totalProducts++
		fmt.Printf("Processing product %v\n\n", item)
		sellingPrice, err := extractPrice(item["sellingPrice"].(string))
		if err != nil {
			log.Println("Failed to extract selling price ", err)
		}
		var priceBeforeDiscount *float64
		if item["priceBeforeDiscount"] != "" {
			price, err := extractPrice(item["priceBeforeDiscount"].(string))
			if err != nil {
				log.Println("Failed to extract price before discount ", err)
			}
			priceBeforeDiscount = &price
		}
		var ratingScore *float64
		var ratingCount *int
		if item["rating"] != "" {
			if score, err := strconv.ParseFloat(item["rating"].(string)[:3], 64); err == nil {
				ratingScore = &score
			}
			if count, err := strconv.Atoi(item["rating"].(string)[3:]); err == nil {
				ratingCount = &count
			}
		}
		var SKU string
		if item["SKU"] != "" {
			SKU, err = extractModelNumber(item["SKU"].(string))
			if err != nil {
				log.Println("Failed to extract model number ", err)
			}
		}

		return Product{
			Category:            extractCategory(item["categories"].(string)),
			Title:               strings.ReplaceAll(item["title"].(string), ",", ""),
			Website:             "noon",
			SellingPrice:        sellingPrice,
			PriceBeforeDiscount: priceBeforeDiscount,
			Currency:            "AED",
			ProductURL:          item["productURL"].(string),
			SKU:                 SKU,
			Brand:               item["brand"].(string),
			RatingScore:         ratingScore,
			RatingCount:         ratingCount,
			Rank:                totalProducts,
		}
	}, 1)).Via(flow.NewMap[Product, string](func(product Product) string {
		return product.ToCSVRow()
	}, 1)).To(sink)

}
