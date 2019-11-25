package main

import (
	"fmt"
	"github.com/jessevdk/go-flags"
	"github.com/PagerDuty/go-pagerduty"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	Author               = "webdevops.io"
	Version              = "0.9.0"
	PAGERDUTY_LIST_LIMIT = 100
)

var (
	argparser            *flags.Parser
	args                 []string
	Verbose              bool
	Logger               *DaemonLogger
	PagerDutyClient      *pagerduty.Client
	collectorGeneralList map[string]*CollectorGeneral
)

var opts struct {
	// general settings
	Verbose []bool `long:"verbose" short:"v"        env:"VERBOSE"                description:"Verbose mode"`

	// server settings
	ServerBind     string        `long:"bind"               env:"SERVER_BIND"            description:"Server address"                                     default:":8080"`
	ScrapeTime     time.Duration `long:"scrape.time"        env:"SCRAPE_TIME"            description:"Scrape time (time.duration)"                        default:"5m"`
	ScrapeTimeLive time.Duration `long:"scrape.time.live"   env:"SCRAPE_TIME_LIVE"       description:"Scrape time incidents and oncalls (time.duration)"  default:"1m"`

	// PagerDuty settings
	PagerDutyAuthToken                 string        `long:"pagerduty.authtoken"                                         env:"PAGERDUTY_AUTH_TOKEN"                         description:"PagerDuty auth token" required:"true"`
	PagerDutyScheduleOverrideTimeframe time.Duration `long:"pagerduty.schedule.override-duration" env:"PAGERDUTY_SCHEDULE_OVERRIDE_TIMEFRAME"        description:"PagerDuty timeframe for fetching schedule overrides (time.Duration)" default:"48h"`
	PagerDutyScheduleEntryTimeframe    time.Duration `long:"pagerduty.schedule.entry-timeframe"      env:"PAGERDUTY_SCHEDULE_ENTRY_TIMEFRAME"           description:"PagerDuty timeframe for fetching schedule entries (time.Duration)" default:"72h"`
	PagerDutyScheduleEntryTimeFormat   string        `long:"pagerduty.schedule.entry-timeformat"           env:"PAGERDUTY_SCHEDULE_ENTRY_TIMEFORMAT"          description:"PagerDuty schedule entry time format (label)" default:"Mon, 02 Jan 15:04 MST"`
	PagerDutyIncidentTimeFormat        string        `long:"pagerduty.incident.timeformat"                      env:"PAGERDUTY_INCIDENT_TIMEFORMAT"                description:"PagerDuty incident time format (label)" default:"Mon, 02 Jan 15:04 MST"`
	PagerDutyDisableCollectors		   []string      `long:"pagerduty.disable-collectors" short:"d"          env:"PAGERDUTY_DISABLE_COLLECTORS"     description:"Collector names to disable checking."`
	PagerDutyTeamListOpt			   []string      `long:"pagerduty.team-listopt"                 env:"PAGERDUTY_TEAM_LISTOPT"            description:"Passes team ID as a list option when applicable."`
}

func main() {
	initArgparser()

	// set verbosity
	Verbose = len(opts.Verbose) >= 1

	// Init logger
	Logger = NewLogger(log.Lshortfile, Verbose)
	defer Logger.Close()

	Logger.Infof("Init Pagerduty exporter v%s (written by %v)", Version, Author)

	Logger.Infof("Init PagerDuty client")
	initPagerDuty()

	Logger.Infof("Starting metrics collection")
	Logger.Infof("  scape time: %v", opts.ScrapeTime)
	Logger.Infof("  scape time live: %v", opts.ScrapeTimeLive)
	initMetricCollector()

	Logger.Infof("Starting http server on %s", opts.ServerBind)
	startHttpServer()
}

// init argparser and parse/validate arguments
func initArgparser() {
	argparser = flags.NewParser(&opts, flags.Default)
	_, err := argparser.Parse()

	// check if there is an parse error
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			fmt.Println(err)
			fmt.Println()
			argparser.WriteHelp(os.Stdout)
			os.Exit(1)
		}
	}
}

// Init and build PagerDuty client
func initPagerDuty() {
	PagerDutyClient = pagerduty.NewClient(opts.PagerDutyAuthToken)
}

func initMetricCollector() {
	var collectorName string
	collectorGeneralList = map[string]*CollectorGeneral{}

	//fmt.Println(opts.PagerDutyDisableCollectors[0])
	//if strings.EqualFold(opts.PagerDutyDisableCollectors[0], "team") {
	//	fmt.Println("contains")
	//}
	//
	//fmt.Println(opts.PagerDutyDisableCollectors)
	//if !contains(opts.PagerDutyDisableCollectors, "team") {
	//	fmt.Println("disabled")
	//}

	var disabled_collectors []string
	if len(opts.PagerDutyDisableCollectors) == 1 {
		disabled_collectors = strings.Split(opts.PagerDutyDisableCollectors[0], ",")
	}

	if !contains(disabled_collectors, "team") {
		collectorName = "Team"
		if opts.ScrapeTime.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorTeam{})
			collectorGeneralList[collectorName].Run(opts.ScrapeTime)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	if !contains(disabled_collectors, "user") {
		collectorName = "User"
		if opts.ScrapeTime.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorUser{teamListOpt: opts.PagerDutyTeamListOpt})
			collectorGeneralList[collectorName].Run(opts.ScrapeTime)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	if !contains(disabled_collectors, "service") {
		collectorName = "Service"
		if opts.ScrapeTime.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorService{teamListOpt: opts.PagerDutyTeamListOpt})
			collectorGeneralList[collectorName].Run(opts.ScrapeTime)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	if !contains(disabled_collectors, "schedule") {
		collectorName = "Schedule"
		if opts.ScrapeTime.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorSchedule{})
			collectorGeneralList[collectorName].Run(opts.ScrapeTime)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	if !contains(disabled_collectors, "maintenancewindow") {
		collectorName = "MaintenanceWindow"
		if opts.ScrapeTime.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorMaintenanceWindow{teamListOpt: opts.PagerDutyTeamListOpt})
			collectorGeneralList[collectorName].Run(opts.ScrapeTime)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	if !contains(disabled_collectors, "oncall") {
		collectorName = "OnCall"
		if opts.ScrapeTimeLive.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorOncall{})
			collectorGeneralList[collectorName].Run(opts.ScrapeTimeLive)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	if !contains(disabled_collectors, "incident") {
		collectorName = "Incident"
		if opts.ScrapeTimeLive.Seconds() > 0 {
			collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorIncident{teamListOpt: opts.PagerDutyTeamListOpt})
			collectorGeneralList[collectorName].Run(opts.ScrapeTimeLive)
		} else {
			Logger.Infof("collector[%s]: disabled", collectorName)
		}
	}

	collectorName = "Collector"
	collectorGeneralList[collectorName] = NewCollectorGeneral(collectorName, &MetricsCollectorCollector{})
	collectorGeneralList[collectorName].Run(time.Duration(10 * time.Second))
	collectorGeneralList[collectorName].SetIsHidden(true)
}

// start and handle prometheus handler
func startHttpServer() {
	http.Handle("/metrics", promhttp.Handler())
	Logger.Fatal(http.ListenAndServe(opts.ServerBind, nil))
}
