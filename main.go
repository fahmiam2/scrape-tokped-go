package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

type Product struct {
	name            string
	price           string
	imageURL        string
	detailProdukURL string
	merchant        string
	rating          string
}

func main() {
	// Step 1: Scraping
	products := scrapeStep1()

	// Print the number of data obtained in step 1
	fmt.Printf("Number of data obtained in Step 1: %d\n", len(products))

	// Step 2: Concurrent scraping for merchant name and rating
	concurrentScrapeStep2(products)

	// Print the number of data obtained in step 2
	fmt.Printf("Number of data obtained in Step 2: %d\n", len(products))

	// Write to CSV
	writeToCSV("scraped_data_concurrentv2_csv.csv", products)

	fmt.Println("Scraping completed and data written to 'scraped_data.csv'.")
}

func scrapeStep1() []Product {
	var products []Product

	chromeOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-http2", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("headless", false),
		chromedp.Flag("start-fullscreen", true),
	)

	ctx, cancel := chromedp.NewExecAllocator(context.Background(), chromeOpts...)
	defer cancel()

	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	log.Println("Navigating to the target page...")
	var productNodes []*cdp.Node
	err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.tokopedia.com/p/handphone-tablet/handphone?page=0"),
		chromedp.Sleep(5000*time.Millisecond),
		chromedp.Nodes(".e1nlzfl2", &productNodes, chromedp.ByQueryAll),
	)
	if err != nil {
		log.Fatal("Error during navigation:", err)
	}

	log.Println("Navigation successful. Waiting for the page content...")

	for _, node := range productNodes {
		var name, price, imageURL, detailProdukURL string
		err = chromedp.Run(ctx,
			chromedp.Text(".css-20kt3o", &name, chromedp.ByQuery, chromedp.FromNode(node)),
			chromedp.Text(".css-pp6b3e", &price, chromedp.ByQuery, chromedp.FromNode(node)),
			chromedp.AttributeValue(`img[class="success fade"]`, "src", &imageURL, nil, chromedp.ByQuery, chromedp.FromNode(node)),
			chromedp.AttributeValue(`a[data-testid="lnkProductContainer"]`, "href", &detailProdukURL, nil, chromedp.ByQuery, chromedp.FromNode(node)),
		)

		if err != nil {
			log.Fatal("Error:", err)
		}

		product := Product{}
		product.imageURL = imageURL
		product.name = name
		product.price = price

		if strings.Contains(detailProdukURL, "r=") {
			decodedDetailURL, err := extractAndDecodeURL(detailProdukURL)
			if err != nil {
				log.Fatal("Error decoding URL:", err)
			}
			product.detailProdukURL = decodedDetailURL
		} else {
			fmt.Println("URL aman")
			product.detailProdukURL = detailProdukURL
		}

		fmt.Printf("Product scraped: %v\n", product)
		products = append(products, product)
	}

	cancel()
	return products
}

func concurrentScrapeStep2(products []Product) {
	chromeOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-http2", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("headless", false),
		chromedp.Flag("start-fullscreen", true),
	)

	ctx, cancel := chromedp.NewExecAllocator(context.Background(), chromeOpts...)
	defer cancel()

	// Create a channel to control the number of concurrent executions
	concurrentLimit := 10
	semaphore := make(chan struct{}, concurrentLimit)

	// Create a channel for errors
	errCh := make(chan error)

	for i := range products {
		semaphore <- struct{}{} // Acquire semaphore

		// Print the detail product URL being processed
		fmt.Printf("Processing detail product URL: %s\n", products[i].detailProdukURL)

		go func(product *Product) {
			defer func() { <-semaphore }() // Release semaphore when done
			err := scrapeStep2(product, ctx)
			if err != nil {
				errCh <- err
			}
		}(&products[i])
	}

	// Wait for all goroutines to finish
	for i := 0; i < concurrentLimit; i++ {
		semaphore <- struct{}{}
	}

	close(errCh)

	// Check for any errors
	for err := range errCh {
		if err != nil {
			log.Fatal("Error during detail scraping:", err)
		}
	}
}

func scrapeStep2(product *Product, ctx context.Context) error {
	detailCtx, cancelDetail := chromedp.NewContext(ctx)

	// Initialize variables to store the scraped data
	var merchant, rating string

	// Scrape merchant name
	err := chromedp.Run(detailCtx,
		chromedp.Navigate(product.detailProdukURL),
		chromedp.WaitVisible(`.css-1wdzqxj-unf-heading`),
		chromedp.Sleep(2000*time.Millisecond),
		chromedp.Text(".css-1wdzqxj-unf-heading", &merchant, chromedp.ByQuery),
	)

	if err != nil {
		// Handle the case where merchant data is not found
		log.Printf("Warning: Merchant data not found for product %v: %v\n", product.name, err)
	} else {
		product.merchant = merchant
	}

	// Scrape rating
	err = chromedp.Run(detailCtx,
		chromedp.Text(`span[data-testid="lblPDPDetailProductRatingNumber"]`, &rating, chromedp.ByQuery),
	)

	if err != nil {
		// Handle the case where rating data is not found
		log.Printf("Warning: Rating data not found for product %v: %v\n", product.name, err)
	} else {
		product.rating = rating
	}

	cancelDetail()
	return nil
}

func extractAndDecodeURL(inputURL string) (string, error) {
	index := strings.Index(inputURL, "r=")

	if index != -1 {
		trimmedURL := inputURL[index+len("r="):]

		decodedURL, err := url.QueryUnescape(trimmedURL)
		if err != nil {
			return "", err
		}

		return decodedURL, nil
	}

	return "", fmt.Errorf("substring not found in the URL")
}

func writeToCSV(filename string, products []Product) {
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal("Error creating CSV file:", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Writing header
	header := []string{"Name", "Price", "ImageURL", "DetailProdukURL", "Merchant", "Rating"}
	if err := writer.Write(header); err != nil {
		log.Fatal("Error writing header to CSV:", err)
	}

	// Writing data
	for _, product := range products {
		row := []string{product.name, product.price, product.imageURL, product.detailProdukURL, product.merchant, product.rating}
		if err := writer.Write(row); err != nil {
			log.Fatal("Error writing data to CSV:", err)
		}
	}
}
