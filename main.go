package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yhat/scrape"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type quote struct {
	Quote  string `json:"quote"`
	Author string `json:"author"`
	Book   string `json:"book"`
}

type quotes []quote

type category struct {
	Name         string  `json:"name"`
	Source       string  `json:"source"`
	Quotes       []quote `json:"quotes,omitempty"`
	QuotesNumber int     `json:"quotes_number"`
	Pages        int     `json:"-"`
}

func gatherThemes() ([]*category, error) {
	var err error
	var root *html.Node
	var resp *http.Response

	matcher := func(n *html.Node) bool {
		return n.DataAtom == atom.Span && scrape.Attr(n, "class") == "name"
	}

	if resp, err = http.Get("http://www.abc-citations.com/themes/"); err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if root, err = html.Parse(resp.Body); err != nil {
		return nil, err
	}
	as := scrape.FindAll(root, matcher)
	r := make([]*category, len(as))
	for i, n := range as {
		r[i] = &category{scrape.Text(n), scrape.Attr(n.Parent, "href"), nil, 0, 0}
	}
	return r, nil
}

func gatherQuotesFromCategory(cat *category, wg *sync.WaitGroup) {
	defer func() {
		log.Printf("Done with '%s' category", cat.Name)
		wg.Done()
	}()
	var err error
	var root *html.Node
	var resp *http.Response
	retries := 0

	if resp, err = http.Get(cat.Source); err != nil {
		ok := false
		for retries < 5 {
			time.Sleep(1 * time.Second)
			if resp, err = http.Get(cat.Source); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			log.Println("Failed for (Get)", cat.Source, err)
			return
		}
	}
	defer resp.Body.Close()
	if root, err = html.Parse(resp.Body); err != nil {
		log.Println("Failed for (Parse)", cat.Source, err)
		return
	}
	as := scrape.FindAll(root, scrape.ByClass("quotation"))
	cat.Quotes = make(quotes, len(as))
	for i, n := range as {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch scrape.Attr(c, "class") {
			case "quote":
				cat.Quotes[i].Quote = scrape.Text(c)
			case "author":
				cat.Quotes[i].Author = scrape.Text(c)
			case "book":
				cat.Quotes[i].Book = scrape.Text(c)
			}
		}
	}
	as = scrape.FindAll(root, scrape.ByClass("wp-pagenavi"))
	if len(as) > 0 {
		next := []string{}
		for _, n := range as {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.DataAtom == atom.A && scrape.Attr(c, "class") != "nextpostslink" {
					next = append(next, scrape.Attr(c, "href"))
				}
			}
		}
		c := make(chan quotes, len(next))
		cat.Pages = len(next) + 1
		done := 0
		for _, n := range next {
			go gatherQuotes(n, c)
		}
		for done != len(next) {
			q := <-c
			if q != nil {
				cat.Quotes = append(cat.Quotes, q...)
			}
			done++
		}
	}
	cat.QuotesNumber = len(cat.Quotes)
}

func gatherQuotes(src string, c chan<- quotes) {
	var err error
	var root *html.Node
	var resp *http.Response
	retries := 0

	if resp, err = http.Get(src); err != nil {
		ok := false
		for retries < 5 {
			time.Sleep(1 * time.Second)
			if resp, err = http.Get(src); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			log.Println("Failed for", src)
			c <- nil
			return
		}
	}
	defer resp.Body.Close()
	if root, err = html.Parse(resp.Body); err != nil {
		log.Println("Failed for", src)
		c <- nil
		return
	}
	as := scrape.FindAll(root, scrape.ByClass("quotation"))
	r := make(quotes, len(as))
	for i, n := range as {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch scrape.Attr(c, "class") {
			case "quote":
				r[i].Quote = scrape.Text(c)
			case "author":
				r[i].Author = scrape.Text(c)
			case "book":
				r[i].Book = scrape.Text(c)
			}
		}
	}
	c <- r
}

func main() {
	start := time.Now()
	themes, err := gatherThemes()
	if err != nil {
		log.Fatal("Error while fetching themes :", err)
	}
	wg := sync.WaitGroup{}
	for _, c := range themes {
		wg.Add(1)
		go gatherQuotesFromCategory(c, &wg)
	}
	wg.Wait()
	m, _ := json.MarshalIndent(themes, "", "    ")
	fmt.Println(string(m))
	ioutil.WriteFile("data.json", m, 0644)
	tp := 0
	tq := 0
	for _, t := range themes {
		tp += t.Pages
		tq += t.QuotesNumber
	}
	log.Printf("Scrapped %d pages (%d quotes) in %v", tp, tq, time.Since(start))
}
