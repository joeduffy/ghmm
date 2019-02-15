package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v19/github"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

var (
	// token will contain the GitHub access token if configured.
	token string
	// yes is used to confirm mutating operations.
	yes bool
)

func main() {
	// Set up the root command to manage milestones across many repos in an organization. Usage examples below.
	c := &cobra.Command{
		Use:   os.Args[0],
		Short: "A tool for managing GitHub milestones",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	c.PersistentFlags().StringVarP(
		&token, "token", "t", "", "GitHub access token (for private repos)")

	// # List all milestones open in the given organization (across all repos):
	// $ ghmm list pulumi
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones in an org or repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("missing repo or organization name")
			}
			return doListMilestones(args[0])
		},
	}
	c.AddCommand(listCmd)

	// # Change a milestone date (across all repos, based on the name):
	// $ ghmm set pulumi '0.20' '1/13/2019'
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set a milestone's date",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("missing repo or organization name")
			} else if len(args) < 2 {
				return errors.New("missing milestone title whose date to set (not its ID)")
			} else if len(args) < 3 {
				return errors.New("missing milestone due date")
			}

			t, err := parseMilestoneDueOn(args[2])
			if err != nil {
				return err
			}

			return doSetMilestone(args[0], args[1], t)
		},
	}
	setCmd.PersistentFlags().BoolVarP(
		&yes, "yes", "y", false, "Actually perform the close operation instead of just dry-running it")
	c.AddCommand(setCmd)

	// # Close a milestone (across all repos, based on the name):
	// $ ghmm close pulumi '0.20'
	closeCmd := &cobra.Command{
		Use:   "close",
		Short: "Close a milestone by name",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("missing repo or organization name")
			} else if len(args) < 2 {
				return errors.New("missing milestone title to close (not its ID)")
			}
			return doCloseMilestone(args[0], args[1])
		},
	}
	closeCmd.PersistentFlags().BoolVarP(
		&yes, "yes", "y", false, "Actually perform the close operation instead of just dry-running it")
	c.AddCommand(closeCmd)

	// # Open a milestone (across all repos, based on the name):
	// $ ghmm open pulumi '0.20' '1/13/2019'
	openCmd := &cobra.Command{
		Use:   "open",
		Short: "Open a milestone with a given name and due date",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("missing repo or organization name")
			} else if len(args) < 2 {
				return errors.New("missing milestone title to open")
			} else if len(args) < 3 {
				return errors.New("missing milestone due date")
			}

			t, err := parseMilestoneDueOn(args[2])
			if err != nil {
				return err
			}

			return doOpenMilestone(args[0], args[1], t)
		},
	}
	openCmd.PersistentFlags().BoolVarP(
		&yes, "yes", "y", false, "Actually perform the open operation instead of just dry-running it")
	c.AddCommand(openCmd)

	// Now run the command.
	if err := c.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func ghClient() *github.Client {
	var tc *http.Client
	if token != "" {
		tc = oauth2.NewClient(
			context.Background(),
			oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
		)
	}
	return github.NewClient(tc)
}

type repo string

func (r repo) Owner() string {
	s := string(r)
	return s[:strings.Index(s, "/")]
}

func (r repo) Repo() string {
	s := string(r)
	return s[strings.Index(s, "/")+1:]
}

func getRepos(gh *github.Client, orgOrRepo string) ([]repo, error) {
	var repos []repo
	if ix := strings.Index(orgOrRepo, "/"); ix != -1 {
		// If just a singular repo, query it directly.
		repos = append(repos, repo(orgOrRepo))
	} else {
		// If an org, use all of the repos in that org. Note that we need to loop to get all pages.
		opts := &github.RepositoryListByOrgOptions{}
		for {
			rs, resp, err := gh.Repositories.ListByOrg(context.Background(), orgOrRepo, opts)
			if err != nil {
				return nil, errors.Wrapf(err, "listing repos by org %s", orgOrRepo)
			}
			for _, r := range rs {
				repos = append(repos, repo(r.GetFullName()))
			}
			if resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
	}
	return repos, nil
}

type milestone struct {
	State string
	DueOn time.Time
	Repos map[repo]bool
}

func (m *milestone) RepoNames() []repo {
	var repos []repo
	for r := range m.Repos {
		repos = append(repos, r)
	}
	return repos
}

func parseMilestoneDueOn(d string) (time.Time, error) {
	t, err := time.Parse("1/2/2006", d)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "malformed date; please use 1/2/2006 format")
	}
	t = t.Add(time.Hour * 8) // All GitHub milestones at 8am.
	return t, nil
}

func doListMilestones(orgOrRepo string) error {
	gh := ghClient()

	// First get the list of repos under consideration.
	repos, err := getRepos(gh, orgOrRepo)
	if err != nil {
		return err
	}

	// Now, for each of them, loop over and query the milestones.
	milestones := make(map[string]*milestone)
	for _, r := range repos {
		ms, _, err := gh.Issues.ListMilestones(context.Background(), r.Owner(), r.Repo(), nil)
		if err != nil {
			return errors.Wrapf(err, "listing milestones for repo %s", r)
		}

		for _, m := range ms {
			t, s, d := m.GetTitle(), m.GetState(), m.GetDueOn()
			exist, ok := milestones[t]
			if ok {
				if exist.State != m.GetState() {
					fmt.Fprintf(os.Stderr,
						"warning: milestone %s in repo %s has a different state "+
							"(has %s, expect %s) than other repos (%v)\n",
						t, r, s, exist.State, exist.RepoNames())
				} else if exist.DueOn != d {
					fmt.Fprintf(os.Stderr,
						"warning: milestone %s in repo %s has a different due date "+
							"(has %v, expect) %v than other repos (%v)\n",
						t, r, d, exist.DueOn, exist.RepoNames())
				}
				exist.Repos[r] = true
			} else {
				milestones[t] = &milestone{
					State: s,
					DueOn: d,
					Repos: map[repo]bool{r: true},
				}
			}
		}
	}

	// Ensure that the full set of repos was accounted for in each milestone and warn if any are missing.
	for t, ms := range milestones {
		for _, repo := range repos {
			if !ms.Repos[repo] {
				fmt.Fprintf(os.Stderr, "warning: milestone %s is missing from repo %s\n", t, repo)
			}
		}
	}

	// Finally actually print out the list of milestones.
	for t, ms := range milestones {
		var repos []string
		for repo := range ms.Repos {
			repos = append(repos, string(repo))
		}
		sort.Strings(repos)
		var repoList string
		for i, repo := range repos {
			if i > 0 {
				repoList += ","
			}
			repoList += repo
		}

		fmt.Printf("%s\t%s\t%v\n", t, ms.DueOn.Format("Mon Jan _2 2006"), repoList)
	}

	return nil
}

func doSetMilestone(orgOrRepo string, milestone string, newDueOn time.Time) error {
	gh := ghClient()

	// First get the list of repos under consideration.
	repos, err := getRepos(gh, orgOrRepo)
	if err != nil {
		return err
	}

	// Now, for each of them, loop over and set the milestones that match.
	c := 0
	for _, r := range repos {
		ms, _, err := gh.Issues.ListMilestones(context.Background(), r.Owner(), r.Repo(), nil)
		if err != nil {
			return errors.Wrapf(err, "listing milestones for repo %s", r)
		}

		for _, m := range ms {
			t, n, s, d := m.GetTitle(), m.GetNumber(), m.GetState(), m.GetDueOn()
			if t == milestone && s == "open" && d != newDueOn {
				if yes {
					m.DueOn = &newDueOn
					_, _, err := gh.Issues.EditMilestone(context.Background(), r.Owner(), r.Repo(), n, m)
					if err != nil {
						return errors.Wrapf(err, "editing milestone %s (#%d) in repo %s", t, n, r)
					}
					fmt.Printf("changed milestone %s (#%d) in repo %s due date from %v to %v\n",
						t, n, r, d, newDueOn)
				} else {
					fmt.Printf("would change milestone %s (#%d) in repo %s due date from %v to %v\n",
						t, n, r, d, newDueOn)
				}

				c++
			}
		}
	}

	if c > 0 {
		if yes {
			fmt.Printf("set %d milestone due dates\n", c)
		} else {
			fmt.Printf("would set %d milestone due dates; re-run with --yes to edit them\n", c)
		}
	}

	return nil
}

func doCloseMilestone(orgOrRepo string, milestone string) error {
	gh := ghClient()

	// First get the list of repos under consideration.
	repos, err := getRepos(gh, orgOrRepo)
	if err != nil {
		return err
	}

	// Now, for each of them, loop over and close the milestones that match.
	c := 0
	for _, r := range repos {
		ms, _, err := gh.Issues.ListMilestones(context.Background(), r.Owner(), r.Repo(), nil)
		if err != nil {
			return errors.Wrapf(err, "listing milestones for repo %s", r)
		}

		for _, m := range ms {
			t, n, s := m.GetTitle(), m.GetNumber(), m.GetState()
			if t == milestone && s == "open" {
				// See if there are any issues open in this milestone.
				opts := &github.IssueListByRepoOptions{Milestone: strconv.Itoa(n)}
				issues, _, err := gh.Issues.ListByRepo(context.Background(), r.Owner(), r.Repo(), opts)
				if err != nil {
					return errors.Wrapf(err, "checking for open milestone %s issues in repo %s", t, r)
				}
				for _, iss := range issues {
					fmt.Fprintf(os.Stderr, "warning: issue #%d in repo %s still active in milestone %s",
						iss.GetNumber(), r, t)
				}

				if yes {
					s = "closed"
					m.State = &s
					_, _, err := gh.Issues.EditMilestone(context.Background(), r.Owner(), r.Repo(), n, m)
					if err != nil {
						return errors.Wrapf(err, "closing milestone %s (#%d) in repo %s", t, n, r)
					}
					fmt.Printf("closed milestone %s (#%d) in repo %s\n", t, n, r)
				} else {
					fmt.Printf("would close milestone %s (#%d) in repo %s\n", t, n, r)
				}

				c++
			}
		}
	}

	if c > 0 {
		if yes {
			fmt.Printf("closed %d milestones\n", c)
		} else {
			fmt.Printf("would close %d milestones; re-run with --yes to close them\n", c)
		}
	}

	return nil
}

func doOpenMilestone(orgOrRepo, milestone string, dueOn time.Time) error {
	// TODO(joe): implement this.
	return errors.New("NYI")
}
