/*
	Provides roll-up statuses for Skia build/test/perf.
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"golang.org/x/net/context"

	"github.com/gorilla/mux"
	"go.skia.org/infra/go/buildbot"
	"go.skia.org/infra/go/common"
	"go.skia.org/infra/go/git/repograph"
	"go.skia.org/infra/go/httputils"
	"go.skia.org/infra/go/login"
	"go.skia.org/infra/go/skiaversion"
	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/go/timer"
	"go.skia.org/infra/go/util"
	"go.skia.org/infra/status/go/capacity"
	"go.skia.org/infra/status/go/franken"
	"go.skia.org/infra/task_scheduler/go/db"
	"go.skia.org/infra/task_scheduler/go/db/local_db"
	"go.skia.org/infra/task_scheduler/go/db/remote_db"
)

const (
	DEFAULT_COMMITS_TO_LOAD = 50
	SKIA_REPO               = "skia"
	INFRA_REPO              = "infra"

	// OAUTH2_CALLBACK_PATH is callback endpoint used for the Oauth2 flow.
	OAUTH2_CALLBACK_PATH = "/oauth2callback/"
)

var (
	buildCache       *franken.BTCache         = nil
	buildDb          buildbot.DB              = nil
	capacityClient   *capacity.CapacityClient = nil
	capacityTemplate *template.Template       = nil
	commitsTemplate  *template.Template       = nil
	tasksPerCommit   *tasksPerCommitCache     = nil
)

// flags
var (
	capacityRecalculateInterval = flag.Duration("capacity_recalculate_interval", 10*time.Minute, "How often to re-calculate capacity statistics.")
	host                        = flag.String("host", "localhost", "HTTP service host")
	port                        = flag.String("port", ":8002", "HTTP service port (e.g., ':8002')")
	promPort                    = flag.String("prom_port", ":20000", "Metrics service address (e.g., ':10110')")
	repoUrls                    = common.NewMultiStringFlag("repo", nil, "Repositories to query for status.")
	resourcesDir                = flag.String("resources_dir", "", "The directory to find templates, JS, and CSS files. If blank the current directory will be used.")
	swarmingUrl                 = flag.String("swarming_url", "https://chromium-swarm.appspot.com", "URL of the Swarming server.")
	taskSchedulerDbUrl          = flag.String("task_db_url", "http://skia-task-scheduler:8008/db/", "Where the Skia task scheduler database is hosted.")
	taskSchedulerUrl            = flag.String("task_scheduler_url", "https://task-scheduler.skia.org", "URL of the Task Scheduler server.")
	testing                     = flag.Bool("testing", false, "Set to true for locally testing rules. No email will be sent.")
	useMetadata                 = flag.Bool("use_metadata", true, "Load sensitive values from metadata not from flags.")
	workdir                     = flag.String("workdir", ".", "Directory to use for scratch work.")

	repos repograph.Map
)

// StringIsInteresting returns true iff the string contains non-whitespace characters.
func StringIsInteresting(s string) bool {
	for _, c := range s {
		if !unicode.IsSpace(c) {
			return true
		}
	}
	return false
}

func reloadTemplates() {
	// Change the current working directory to two directories up from this source file so that we
	// can read templates and serve static (res/) files.

	if *resourcesDir == "" {
		_, filename, _, _ := runtime.Caller(0)
		*resourcesDir = filepath.Join(filepath.Dir(filename), "../..")
	}
	commitsTemplate = template.Must(template.ParseFiles(
		filepath.Join(*resourcesDir, "templates/commits.html"),
		filepath.Join(*resourcesDir, "templates/header.html"),
	))
	capacityTemplate = template.Must(template.ParseFiles(
		filepath.Join(*resourcesDir, "templates/capacity.html"),
		filepath.Join(*resourcesDir, "templates/header.html"),
	))
}

func Init() {
	reloadTemplates()
}

func userHasEditRights(r *http.Request) bool {
	return strings.HasSuffix(login.LoggedInAs(r), "@google.com")
}

func getIntParam(name string, r *http.Request) (*int, error) {
	raw, ok := r.URL.Query()[name]
	if !ok {
		return nil, nil
	}
	v64, err := strconv.ParseInt(raw[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("Invalid integer value for parameter %q", name)
	}
	v32 := int(v64)
	return &v32, nil
}

// repoUrlToName returns a short repo nickname given a full repo URL.
func repoUrlToName(repoUrl string) string {
	// Special case: we like "infra" better than "buildbot".
	if repoUrl == common.REPO_SKIA_INFRA {
		return "infra"
	}
	return strings.TrimSuffix(path.Base(repoUrl), ".git")
}

// repoNameToUrl returns a full repo URL given a short nickname, or an error
// if no matching repo URL is found.
func repoNameToUrl(repoName string) (string, error) {
	// Special case: we like "infra" better than "buildbot".
	if repoName == "infra" {
		return common.REPO_SKIA_INFRA, nil
	}
	// Search the list of repos used by this server.
	for _, repoUrl := range *repoUrls {
		if repoUrlToName(repoUrl) == repoName {
			return repoUrl, nil
		}
	}
	return "", fmt.Errorf("No such repo.")
}

// getRepo returns a short repo nickname and a full repo URL based on the URL
// path of the given http.Request.
func getRepo(r *http.Request) (string, string, error) {
	repoPath, _ := mux.Vars(r)["repo"]
	repoUrl, err := repoNameToUrl(repoPath)
	if err != nil {
		return "", "", err
	}
	return repoUrlToName(repoUrl), repoUrl, nil
}

// getRepoNames returns the nicknames for all repos on this server.
func getRepoNames() []string {
	repoNames := make([]string, 0, len(*repoUrls))
	for _, repoUrl := range *repoUrls {
		repoNames = append(repoNames, repoUrlToName(repoUrl))
	}
	return repoNames
}

// commitsJsonHandler writes information about a range of commits into the
// ResponseWriter. The information takes the form of a JSON-encoded CommitsData
// object.
func commitsJsonHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("commitsJsonHandler").Stop()
	w.Header().Set("Content-Type", "application/json")
	commitsToLoad := DEFAULT_COMMITS_TO_LOAD
	n, err := getIntParam("n", r)
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Invalid parameter: %v", err))
		return
	}
	if n != nil {
		commitsToLoad = *n
	}
	// Prevent server overload.
	if commitsToLoad > franken.MAX_COMMITS_TO_LOAD {
		commitsToLoad = franken.MAX_COMMITS_TO_LOAD
	}
	if commitsToLoad < 0 {
		commitsToLoad = DEFAULT_COMMITS_TO_LOAD
	}
	_, repoUrl, err := getRepo(r)
	if err != nil {
		httputils.ReportError(w, r, err, err.Error())
		return
	}
	bc, err := buildCache.GetLastN(repoUrl, commitsToLoad, login.IsGoogler(r))
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to load commits from cache: %v", err))
		return
	}
	rv := struct {
		*franken.CommitsData
		TaskSchedulerUrl string `json:"task_scheduler_url"`
	}{
		CommitsData:      bc,
		TaskSchedulerUrl: *taskSchedulerUrl,
	}
	if err := json.NewEncoder(w).Encode(rv); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to encode response: %s", err))
		return
	}
}

func addBuildCommentHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("addBuildCommentHandler").Stop()
	if !userHasEditRights(r) {
		httputils.ReportError(w, r, fmt.Errorf("User does not have edit rights."), "User does not have edit rights.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	master, ok := mux.Vars(r)["master"]
	if !ok {
		httputils.ReportError(w, r, fmt.Errorf("No build master given!"), "No build master given!")
		return
	}
	builder, ok := mux.Vars(r)["builder"]
	if !ok {
		httputils.ReportError(w, r, fmt.Errorf("No builder given!"), "No builder given!")
		return
	}
	number, err := strconv.ParseInt(mux.Vars(r)["number"], 10, 64)
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("No valid build number given: %v", err))
		return
	}

	comment := struct {
		Comment string `json:"comment"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&comment); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to add comment: %v", err))
		return
	}
	defer util.Close(r.Body)
	c := buildbot.BuildComment{
		User:      login.LoggedInAs(r),
		Timestamp: time.Now().UTC(),
		Message:   comment.Comment,
	}
	if err := buildCache.AddBuildComment(master, builder, int(number), &c); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to add comment: %v", err))
		return
	}
}

func deleteBuildCommentHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("deleteBuildCommentHandler").Stop()
	if !userHasEditRights(r) {
		httputils.ReportError(w, r, fmt.Errorf("User does not have edit rights."), "User does not have edit rights.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	master, ok := mux.Vars(r)["master"]
	if !ok {
		httputils.ReportError(w, r, fmt.Errorf("No build master given!"), "No build master given!")
		return
	}
	builder, ok := mux.Vars(r)["builder"]
	if !ok {
		httputils.ReportError(w, r, fmt.Errorf("No builder given!"), "No builder given!")
		return
	}
	number, err := strconv.ParseInt(mux.Vars(r)["number"], 10, 64)
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("No valid build number given: %v", err))
		return
	}
	commentId, err := strconv.ParseInt(mux.Vars(r)["commentId"], 10, 64)
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Invalid comment id: %v", err))
		return
	}
	if err := buildCache.DeleteBuildComment(master, builder, int(number), commentId); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to delete comment: %v", err))
		return
	}
}

func addBuilderCommentHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("addBuilderCommentHandler").Stop()
	if !userHasEditRights(r) {
		httputils.ReportError(w, r, fmt.Errorf("User does not have edit rights."), "User does not have edit rights.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	builder := mux.Vars(r)["builder"]

	comment := struct {
		Comment       string `json:"comment"`
		Flaky         bool   `json:"flaky"`
		IgnoreFailure bool   `json:"ignoreFailure"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&comment); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to add comment: %v", err))
		return
	}
	defer util.Close(r.Body)

	c := buildbot.BuilderComment{
		Builder:       builder,
		User:          login.LoggedInAs(r),
		Timestamp:     time.Now().UTC(),
		Flaky:         comment.Flaky,
		IgnoreFailure: comment.IgnoreFailure,
		Message:       comment.Comment,
	}
	if err := buildCache.AddBuilderComment(builder, &c); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to add builder comment: %v", err))
		return
	}
}

func deleteBuilderCommentHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("deleteBuilderCommentHandler").Stop()
	if !userHasEditRights(r) {
		httputils.ReportError(w, r, fmt.Errorf("User does not have edit rights."), "User does not have edit rights.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	builder := mux.Vars(r)["builder"]
	commentId, err := strconv.ParseInt(mux.Vars(r)["commentId"], 10, 32)
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Invalid comment id: %v", err))
		return
	}
	if err := buildCache.DeleteBuilderComment(builder, commentId); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to delete comment: %v", err))
		return
	}
}

func addCommitCommentHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("addCommitCommentHandler").Stop()
	if !userHasEditRights(r) {
		httputils.ReportError(w, r, fmt.Errorf("User does not have edit rights."), "User does not have edit rights.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, repoUrl, err := getRepo(r)
	if err != nil {
		httputils.ReportError(w, r, err, err.Error())
		return
	}
	commit := mux.Vars(r)["commit"]
	comment := struct {
		Comment       string `json:"comment"`
		IgnoreFailure bool   `json:"ignoreFailure"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&comment); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to add comment: %v", err))
		return
	}
	defer util.Close(r.Body)

	c := buildbot.CommitComment{
		Commit:        commit,
		User:          login.LoggedInAs(r),
		Timestamp:     time.Now().UTC(),
		IgnoreFailure: comment.IgnoreFailure,
		Message:       comment.Comment,
	}
	if err := buildCache.AddCommitComment(repoUrl, &c); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to add commit comment: %s", err))
		return
	}
}

func deleteCommitCommentHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("deleteCommitCommentHandler").Stop()
	if !userHasEditRights(r) {
		httputils.ReportError(w, r, fmt.Errorf("User does not have edit rights."), "User does not have edit rights.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, repoUrl, err := getRepo(r)
	if err != nil {
		httputils.ReportError(w, r, err, err.Error())
		return
	}
	commit := mux.Vars(r)["commit"]
	commentId, err := strconv.ParseInt(mux.Vars(r)["commentId"], 10, 64)
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Invalid comment id: %v", err))
		return
	}
	if err := buildCache.DeleteCommitComment(repoUrl, commit, commentId); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to delete commit comment: %s", err))
		return
	}
}

type commitsTemplateData struct {
	Repo     string
	Title    string
	RepoBase string
	Repos    []string
}

func defaultRedirectHandler(w http.ResponseWriter, r *http.Request) {
	defaultRepo := repoUrlToName((*repoUrls)[0])
	http.Redirect(w, r, fmt.Sprintf("/repo/%s", defaultRepo), http.StatusFound)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("commitsHandler").Stop()
	w.Header().Set("Content-Type", "text/html")

	repoName, repoUrl, err := getRepo(r)
	if err != nil {
		httputils.ReportError(w, r, err, err.Error())
		return
	}

	// Don't use cached templates in testing mode.
	if *testing {
		reloadTemplates()
	}

	d := commitsTemplateData{
		Repo:     repoName,
		RepoBase: fmt.Sprintf("%s/+/", repoUrl),
		Repos:    getRepoNames(),
		Title:    fmt.Sprintf("Status: %s", repoName),
	}

	if err := commitsTemplate.Execute(w, d); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to expand template: %v", err))
	}
}

func capacityHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("capacityHandler").Stop()
	w.Header().Set("Content-Type", "text/html")

	// Don't use cached templates in testing mode.
	if *testing {
		reloadTemplates()
	}

	page := struct {
		Repos []string
	}{
		Repos: getRepoNames(),
	}
	if err := capacityTemplate.Execute(w, page); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to expand template: %v", err))
	}
}

func capacityStatsHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("capacityStatsHandler").Stop()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(capacityClient.CapacityMetrics()); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to encode response: %s", err))
		return
	}
}

// buildProgressHandler returns the number of finished builds at the given
// commit, compared to that of an older commit.
func buildProgressHandler(w http.ResponseWriter, r *http.Request) {
	defer timer.New("buildProgressHandler").Stop()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get the number of finished tasks for the requested commit.
	hash := r.FormValue("commit")
	if !util.ValidateCommit(hash) {
		httputils.ReportError(w, r, nil, fmt.Sprintf("%q is not a valid commit hash.", hash))
		return
	}
	_, repoUrl, err := getRepo(r)
	if err != nil {
		httputils.ReportError(w, r, err, err.Error())
		return
	}
	builds, err := buildCache.GetBuildsForCommit(repoUrl, hash, login.IsGoogler(r))
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to get the number of finished builds."))
		return
	}
	finished := 0
	for _, b := range builds {
		if b.Finished {
			finished++
		}
	}
	tasksForCommit, err := tasksPerCommit.Get(db.RepoState{
		Repo:     repoUrl,
		Revision: hash,
	})
	if err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to get number of tasks at commit."))
		return
	}
	proportion := 1.0
	if tasksForCommit > 0 {
		proportion = float64(finished) / float64(tasksForCommit)
	}

	res := struct {
		Commit             string  `json:"commit"`
		FinishedTasks      int     `json:"finishedTasks"`
		FinishedProportion float64 `json:"finishedProportion"`
		TotalTasks         int     `json:"totalTasks"`
	}{
		Commit:             hash,
		FinishedTasks:      finished,
		FinishedProportion: proportion,
		TotalTasks:         tasksForCommit,
	}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		httputils.ReportError(w, r, err, fmt.Sprintf("Failed to encode JSON."))
		return
	}
}

func runServer(serverURL string) {
	r := mux.NewRouter()
	r.HandleFunc("/", defaultRedirectHandler)
	r.HandleFunc("/repo/{repo}", statusHandler)
	r.HandleFunc("/capacity", capacityHandler)
	r.HandleFunc("/capacity/json", capacityStatsHandler)
	r.HandleFunc("/json/version", skiaversion.JsonHandler)
	r.HandleFunc("/json/{repo}/buildProgress", buildProgressHandler)
	r.HandleFunc("/logout/", login.LogoutHandler)
	r.HandleFunc("/loginstatus/", login.StatusHandler)
	r.HandleFunc(OAUTH2_CALLBACK_PATH, login.OAuth2CallbackHandler)
	r.PathPrefix("/res/").HandlerFunc(httputils.MakeResourceHandler(*resourcesDir))
	builds := r.PathPrefix("/json/{repo}/builds/{master}/{builder}/{number:[0-9]+}").Subrouter()
	builds.HandleFunc("/comments", addBuildCommentHandler).Methods("POST")
	builds.HandleFunc("/comments/{commentId:[0-9]+}", deleteBuildCommentHandler).Methods("DELETE")
	builders := r.PathPrefix("/json/{repo}/builders/{builder}").Subrouter()
	builders.HandleFunc("/comments", addBuilderCommentHandler).Methods("POST")
	builders.HandleFunc("/comments/{commentId:[0-9]+}", deleteBuilderCommentHandler).Methods("DELETE")
	commits := r.PathPrefix("/json/{repo}/commits").Subrouter()
	commits.HandleFunc("/", commitsJsonHandler)
	commits.HandleFunc("/{commit:[a-f0-9]+}/comments", addCommitCommentHandler).Methods("POST")
	commits.HandleFunc("/{commit:[a-f0-9]+}/comments/{commentId:[0-9]+}", deleteCommitCommentHandler).Methods("DELETE")
	http.Handle("/", httputils.LoggingGzipRequestResponse(r))
	sklog.Infof("Ready to serve on %s", serverURL)
	sklog.Fatal(http.ListenAndServe(*port, nil))
}

func main() {
	defer common.LogPanic()
	// Setup flags.

	common.InitWithMust(
		"status",
		common.PrometheusOpt(promPort),
		common.CloudLoggingOpt(),
	)

	v, err := skiaversion.GetVersion()
	if err != nil {
		sklog.Fatal(err)
	}
	sklog.Infof("Version %s, built at %s", v.Commit, v.Date)

	Init()
	if *testing {
		*useMetadata = false
	}
	serverURL := "https://" + *host
	if *testing {
		serverURL = "http://" + *host + *port
	}

	// Create remote Tasks DB.
	var taskDb db.RemoteDB
	if *testing {
		taskDb, err = local_db.NewDB("status-testing", path.Join(*workdir, "status-testing.bdb"))
		if err != nil {
			sklog.Fatal(err)
		}
		defer util.Close(taskDb.(db.DBCloser))
	} else {
		taskDb, err = remote_db.NewClient(*taskSchedulerDbUrl)
		if err != nil {
			sklog.Fatal(err)
		}
	}

	login.SimpleInitMust(*port, *testing)

	// Check out source code.
	reposDir := path.Join(*workdir, "repos")
	if err := os.MkdirAll(reposDir, os.ModePerm); err != nil {
		sklog.Fatal(err)
	}
	if *repoUrls == nil {
		*repoUrls = common.PUBLIC_REPOS
	}
	repos, err = repograph.NewMap(*repoUrls, reposDir)
	if err != nil {
		sklog.Fatal(err)
	}
	sklog.Info("Checkout complete")

	// Cache for buildProgressHandler.
	tasksPerCommit, err = newTasksPerCommitCache(*workdir, []string{common.REPO_SKIA, common.REPO_SKIA_INFRA}, 14*24*time.Hour, context.Background())
	if err != nil {
		sklog.Fatalf("Failed to create tasksPerCommitCache: %s", err)
	}

	// Create the build cache.
	bc, err := franken.NewBTCache(repos, taskDb, *swarmingUrl, *taskSchedulerUrl)
	if err != nil {
		sklog.Fatalf("Failed to create build cache: %s", err)
	}
	buildCache = bc

	capacityClient = capacity.New(tasksPerCommit.tcc, bc.GetTaskCache(), repos)
	capacityClient.StartLoading(*capacityRecalculateInterval)

	// Run the server.
	runServer(serverURL)
}
