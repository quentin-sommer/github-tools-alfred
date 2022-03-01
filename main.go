package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/deanishe/awgo/keychain"
	"github.com/google/go-github/v41/github"

	aw "github.com/deanishe/awgo"
)

const KeychainAccessTokenKey string = "qsmr_alfred_workflow_gh_access_token"

var (
	maxResults = 200 // Number of results sent to Alfred

	// Command-line flags
	doDownloadFlag bool

	wf *aw.Workflow
)

func init() {
	flag.BoolVar(&doDownloadFlag, "download", false, "retrieve list of workflows from GitHub")

	wf = aw.New(aw.HelpURL("http://www.deanishe.net/"),
		aw.MaxResults(maxResults))
}

type Command func(doDownload bool, query string)

func prs(doDownload bool, query string) {
	maxCacheAge := 1 * time.Minute
	cacheName := "prs.json"
	log.Printf("[main] Fetching prs and filtering with query '%s'", query)
	if doDownload {
		cacheData(cacheName, fetchPrs)
	}
	var prs []*PullRequest
	value, expired := getCachedValue(cacheName, maxCacheAge)

	if value != nil {
		if err := json.Unmarshal(*value, &prs); err != nil {
			wf.FatalError(err)
		}
	}
	if expired {
		startDownloadJob("prs")
	}
	if prs == nil {
		wf.NewItem("Downloading pull requests…")
		return
	}

	for _, pr := range prs {
		wf.NewItem(pr.GetTitle()).
			Subtitle(pr.GetHTMLURL()).
			Arg(pr.GetHTMLURL()).
			UID(strconv.FormatInt(pr.GetID(), 10)).
			Valid(true)
	}

	if query != "" {
		res := wf.Filter(query)
		log.Printf("[main] %d/%d prs match %q", len(res), len(prs), query)
	}
}

func getCachedValue(key string, ttl time.Duration) (cached *[]byte, expired bool) {
	if wf.Cache.Exists(key) {
		value, err := wf.Cache.Load(key)
		if err != nil {
			wf.FatalError(err)
		}
		if wf.Cache.Expired(key, ttl) {
			return &value, true
		}
		return &value, false
	} else {
		return nil, true
	}
}
func isLoggedIn() bool {
	_, err := wf.Keychain.Get(KeychainAccessTokenKey)
	return err == nil
}

func startDownloadJob(name string) {
	wf.Rerun(0.5)

	if !wf.IsRunning("download-" + name) {
		if !isLoggedIn() {
			wf.Fatal("You must first login with `gh-login`")
		}

		cmd := exec.Command(os.Args[0], append([]string{"-download"}, wf.Args()...)...)
		log.Println(cmd)
		if err := wf.RunInBackground("download-"+name, cmd); err != nil {
			wf.FatalError(err)
		}
	} else {
		log.Printf("download-" + name + " job already running.")
	}
}

func cacheData(key string, fetcher func() (interface{}, error)) {
	wf.Configure(aw.TextErrors(true))
	funcName := runtime.FuncForPC(reflect.ValueOf(fetcher).Pointer()).Name()
	log.Printf("[main] downloading " + funcName)
	data, err := fetcher()
	if err != nil {
		wf.FatalError(err)
	}
	if err := wf.Cache.StoreJSON(key, data); err != nil {
		wf.FatalError(err)
	}
	log.Printf("[main] downloaded " + funcName)
}

func repos(doDownload bool, query string) {
	maxCacheAge := 5 * time.Second
	cacheName := "repos.json"
	log.Printf("[main] Fetching repositories and filtering with query '%s'", query)

	if doDownload {
		cacheData(cacheName, fetchAccessibleRepos)
		return
	}

	var repos []*github.Repository
	value, expired := getCachedValue(cacheName, maxCacheAge)
	if value != nil {
		if err := json.Unmarshal(*value, &repos); err != nil {
			wf.FatalError(err)
		}
	}
	if expired {
		startDownloadJob("repos")
	}
	if repos == nil {
		wf.NewItem("Downloading repos…").
			Icon(aw.IconInfo)
		return
	}

	for _, r := range repos {
		sub := fmt.Sprintf("★ %d", r.GetStargazersCount())
		desc := r.GetDescription()
		if desc != "" {
			sub += " – " + desc
		}
		wf.NewItem(r.GetFullName()).
			Subtitle(sub).
			Arg(r.GetHTMLURL()).
			UID(r.GetFullName()).
			Valid(true)
	}

	if query != "" {
		res := wf.Filter(query)
		log.Printf("[main] %d/%d repos match %q", len(res), len(repos), query)
	}
}

func login(_ bool, query string) {
	// Perform login
	if query != "" {
		client, ctx := getGitHubClientWithToken(query)
		u, _, err := client.Users.Get(ctx, "")
		if err != nil {
			wf.FatalError(err)
			return
		}
		if err := wf.Keychain.Set(KeychainAccessTokenKey, query); err != nil {
			wf.FatalError(err)
		} else {
			wf.NewItem("Successfully Logged in with user " + u.GetLogin())
		}
		// Verify login state
	} else {
		token, err := wf.Keychain.Get(KeychainAccessTokenKey)
		if err != nil {
			if err == keychain.ErrNotFound {
				wf.NewItem("Paste your personal access token")
				return
			}
			wf.FatalError(err)
			return
		}
		client, ctx := getGitHubClientWithToken(token)
		user, _, err := client.Users.Get(ctx, "")
		if err != nil {
			wf.FatalError(err)
		}
		wf.NewItem("Already logged in as user " + user.GetLogin())
	}

}

var commands = map[string]Command{
	"repos":  repos,
	"prs":    prs,
	"login":  login,
	"logout": logout,
}

func logout(_ bool, _ string) {
	if err := wf.Keychain.Delete(KeychainAccessTokenKey); err != nil {
		if err != keychain.ErrNotFound {
			wf.FatalError(err)
		} else {
			wf.NewItem("You are not currently logged in")
		}
	} else {
		wf.NewItem("Successfully deleted your personal access token")
	}
}

func run() {
	wf.Args() // call to handle any magic actions
	flag.Parse()
	if args := flag.Args(); len(args) > 0 {
		command := args[0]
		query := ""
		if len(args) > 1 {
			query = args[1]
		}
		commands[command](doDownloadFlag, query)
	} else {
		wf.Fatal("Could not parse arguments: " + strings.Join(args, ", "))
	}
	// Convenience method that shows a warning if there are no results to show.
	// Alfred's default behaviour if no results are returned is to show its
	// fallback searches, which is also what it does if a workflow errors out.
	//
	// As such, it's a good idea to display a message in this situation,
	// otherwise the user can't tell if the workflow failed or simply found
	// no matching results.
	wf.WarnEmpty("No results found", "Try a different query?")

	// Send results/warning message to Alfred
	wf.SendFeedback()
}

func main() {
	wf.Run(run)
}
