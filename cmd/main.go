package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/radoslavboychev/gophercises-hn/hn"
)

type item struct {
	Host string `json:"host,omitempty"`
	hn.Item
}

type templateData struct {
	Stories []item        `json:"stories,omitempty"`
	Time    time.Duration `json:"time,omitempty"`
}

type result struct {
	idx  int
	item item
	err  error
}

var (
	cache           []item
	cacheExpiration time.Time
	cacheMutex      sync.Mutex
)

func main() {

	// flags to specify server port and amount of stories
	var port, numStories int
	flag.IntVar(&port, "port", 3000, "port to host server on")
	flag.IntVar(&numStories, "num_stories", 30, "number of stories to display")
	flag.Parse()

	// parse gohtml template
	tpl := template.Must(template.ParseFiles(".././html/index.gohtml"))

	// handler
	http.HandleFunc("/", handler(numStories, tpl))

	// serve
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

// handles the routing
func handler(numStories int, tpl *template.Template) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		stories, err := getCachedStories(numStories)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		data := templateData{
			Stories: stories,
			Time:    time.Since(start),
		}

		err = tpl.Execute(w, data)
		if err != nil {
			http.Error(w, "failed to process the template", http.StatusInternalServerError)
			return
		}
	})
}

// returns stories from cache
func getCachedStories(numStories int) ([]item, error) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	if time.Since(cacheExpiration) < 0 {
		return cache, nil
	}
	stories, err := getTopStories(numStories)
	if err != nil {
		return nil, err
	}
	cache = stories
	cacheExpiration = time.Now().Add(5 * time.Minute)
	return cache, nil
}

// get the stories
func getTopStories(numStories int) ([]item, error) {
	var client hn.Client
	ids, err := client.TopItems()
	if err != nil {
		return nil, errors.New("failed to load top stories")
	}
	var stories []item
	at := 0
	for len(stories) < numStories {
		need := (numStories - len(stories)) * 5 / 4
		stories = append(stories, getStories(ids[at:at+need])...)
		at += need
	}
	return stories[:numStories], nil
}

// getStories returns
func getStories(ids []int) []item {
	resultCh := make(chan result)
	for i := 0; i < len(ids); i++ {
		go func(idx, id int) {
			var client hn.Client
			hnItem, err := client.GetItem(id)
			if err != nil {
				resultCh <- result{idx: idx, err: err}
			}
			resultCh <- result{idx: idx, item: parseHNItem(hnItem)}
		}(i, ids[i])
	}

	var results []result
	for i := 0; i < len(ids); i++ {
		results = append(results, <-resultCh)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].idx < results[j].idx
	})

	var stories []item
	for _, res := range results {
		if res.err != nil {
			continue
		}
		if isStoryLink(res.item) {
			stories = append(stories, res.item)
		}
	}
	return stories
}

func isStoryLink(item item) bool {
	return item.Type == "story" && item.URL != ""
}

func parseHNItem(hnItem hn.Item) item {
	ret := item{Item: hnItem}
	url, err := url.Parse(ret.URL)
	if err == nil {
		ret.Host = strings.TrimPrefix(url.Hostname(), "www.")
	}
	return ret
}
