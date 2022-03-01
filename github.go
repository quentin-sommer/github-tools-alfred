package main

import (
	"context"
	"log"
	"os"

	"github.com/deanishe/awgo/keychain"
	"github.com/google/go-github/v41/github"
	"golang.org/x/oauth2"
)

type PullRequest = github.Issue

func fetchPrs() (interface{}, error) {
	client, ctx := getGitHubClient()
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		wf.FatalError(err)
	}
	prs, _, err := client.Search.Issues(ctx, "is:pr state:open author:"+user.GetLogin(), &github.SearchOptions{
		Sort:  "updated",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	})

	if err != nil {
		wf.FatalError(err)
	}
	return prs.Issues, nil
}

func getGitHubClientWithToken(accessToken string) (*github.Client, context.Context) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: accessToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc), context.Background()
}

func getGitHubClient() (*github.Client, context.Context) {
	ghToken, err := wf.Keychain.Get(KeychainAccessTokenKey)
	if err != nil {
		if err == keychain.ErrNotFound {
			wf.Fatal("Login first using `gh-login` before using this command")
		} else {
			wf.FatalError(err)
		}
	}
	return getGitHubClientWithToken(ghToken)
}

func fetchRepos(pageNumber int, client *github.Client, ctx context.Context) ([]*github.Repository, *github.Response, error) {
	opts := github.RepositoryListOptions{
		Sort:      "pushed",
		Direction: "desc",
		ListOptions: github.ListOptions{
			Page:    pageNumber,
			PerPage: 100,
		},
	}
	fetchedRepos, resp, err := client.Repositories.List(ctx, "", &opts)

	log.Println("Fetched page", pageNumber, "with", len(fetchedRepos), "repos")
	return fetchedRepos, resp, err
}

type RepoFetch struct {
	repos            []*github.Repository
	lastPagedReached bool
	error            error
}

func fetchAccessibleRepos() (interface{}, error) {
	client, ctx := getGitHubClient()
	firstBatchSize := 4
	channel := make(chan RepoFetch)
	lastPageReached := false
	var repos []*github.Repository

	log.Printf("Fetching %d first pages concurrently", firstBatchSize)
	for i := 0; i < firstBatchSize; i++ {
		go func(pageNumber int) {
			log.SetOutput(os.Stderr)
			fetchedRepos, resp, err := fetchRepos(pageNumber, client, ctx)
			channel <- RepoFetch{
				repos:            fetchedRepos,
				lastPagedReached: resp.LastPage == 0,
				error:            err,
			}
		}(i + 1)
	}
	for i := 0; i < firstBatchSize; i++ {
		res := <-channel
		if res.error != nil {
			wf.FatalError(res.error)
		}
		repos = append(repos, res.repos...)
		if res.lastPagedReached {
			lastPageReached = true
		}

	}

	if lastPageReached {
		return repos, nil
	}
	log.Println("Fetching subsequent pages sequentially")
	page := firstBatchSize + 1
	for page != 0 {
		fetchedRepos, resp, err := fetchRepos(page, client, ctx)
		if err != nil {
			wf.FatalError(err)
		}
		repos = append(repos, fetchedRepos...)
		page = resp.NextPage
	}

	return repos, nil
}
