package client

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/olekukonko/tablewriter"
	"github.com/sapcc/hermes-ctl/audit/v1/events"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var defaultListKeyOrder = []string{
	"ID",
	"Time",
	"Source",
	"Action",
	"Outcome",
	"Target",
	"Initiator",
}

func parseTime(timeStr string) (time.Time, error) {
	validTimeFormats := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04:05-0700"}
	var t time.Time
	var err error
	for _, timeFormat := range validTimeFormats {
		t, err = time.Parse(timeFormat, timeStr)
		if err == nil {
			return t, nil
		}
	}
	return time.Now(), err
}

// ListCmd represents the list command
var ListCmd = &cobra.Command{
	Use:   "list",
	Args:  cobra.ExactArgs(0),
	Short: "List Hermes events",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		viper.BindPFlag("initiator-id", cmd.Flags().Lookup("initiator-id"))
		viper.BindPFlag("initiator-name", cmd.Flags().Lookup("initiator-name"))
		viper.BindPFlag("target-type", cmd.Flags().Lookup("target-type"))
		viper.BindPFlag("target-id", cmd.Flags().Lookup("target-id"))
		viper.BindPFlag("action", cmd.Flags().Lookup("action"))
		viper.BindPFlag("outcome", cmd.Flags().Lookup("outcome"))
		viper.BindPFlag("source", cmd.Flags().Lookup("source"))
		viper.BindPFlag("time", cmd.Flags().Lookup("time"))
		viper.BindPFlag("time-start", cmd.Flags().Lookup("time-start"))
		viper.BindPFlag("time-end", cmd.Flags().Lookup("time-end"))
		viper.BindPFlag("limit", cmd.Flags().Lookup("limit"))
		viper.BindPFlag("sort", cmd.Flags().Lookup("sort"))

		// check time flag
		teq := viper.GetString("time")
		tgt := viper.GetString("time-start")
		tlt := viper.GetString("time-end")
		if teq != "" && !(tgt == "" && tlt == "") {
			return fmt.Errorf("Cannot combine time flag with time-start or time-end flags")
		}

		return verifyGlobalFlags(defaultListKeyOrder)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// list events
		client, err := NewHermesV1Client()
		if err != nil {
			return fmt.Errorf("Failed to create Hermes client: %s", err)
		}

		limit := viper.GetInt("limit")
		keyOrder := viper.GetStringSlice("column")
		if len(keyOrder) == 0 {
			keyOrder = defaultListKeyOrder
		}
		format := viper.GetString("format")

		listOpts := events.ListOpts{
			Limit:         limit,
			TargetType:    viper.GetString("target-type"),
			TargetID:      viper.GetString("target-id"),
			InitiatorID:   viper.GetString("initiator-id"),
			InitiatorName: viper.GetString("initiator-name"),
			Action:        viper.GetString("action"),
			Outcome:       viper.GetString("outcome"),
			ObserverType:  viper.GetString("source"),
			// TODO: verify why only time sort works in hermes server
			Sort: strings.Join(viper.GetStringSlice("sort"), ","),
		}

		if limit == 0 {
			// default per page limit
			listOpts.Limit = 5000
		}

		if t := viper.GetString("time"); t != "" {
			rt, err := parseTime(t)
			if err != nil {
				return fmt.Errorf("Failed to parse time: %s", err)
			}
			listOpts.Time = []events.DateQuery{
				{
					Date: rt,
				},
			}
		}
		if t := viper.GetString("time-start"); t != "" {
			rt, err := parseTime(t)
			if err != nil {
				return fmt.Errorf("Failed to parse time-start: %s", err)
			}
			listOpts.Time = append(listOpts.Time, events.DateQuery{
				Date:   rt,
				Filter: events.DateFilterGTE,
			})
		}
		if t := viper.GetString("time-end"); t != "" {
			rt, err := parseTime(t)
			if err != nil {
				return fmt.Errorf("Failed to parse time-end: %s", err)
			}
			listOpts.Time = append(listOpts.Time, events.DateQuery{
				Date:   rt,
				Filter: events.DateFilterLTE,
			})
		}

		var allEvents []events.Event

		err = events.List(client, listOpts).EachPage(func(page pagination.Page) (bool, error) {
			evnts, err := events.ExtractEvents(page)
			if err != nil {
				return false, fmt.Errorf("Failed to extract events: %s", err)
			}

			allEvents = append(allEvents, evnts...)

			if limit > 0 && len(allEvents) >= limit {
				// break the loop, when output limit is reached
				return false, nil
			}

			return true, nil
		})
		if err != nil {
			if _, ok := err.(gophercloud.ErrDefault500); ok {
				return fmt.Errorf(`Failed to list events: %s: please try to decrease an amount of the events in output, e.g. set "--limit 100"`, err)
			}
			return fmt.Errorf("Failed to list events: %s", err)
		}

		if format == "table" {
			var buf bytes.Buffer
			table := tablewriter.NewWriter(&buf)
			table.SetColWidth(20)
			table.SetAlignment(3)
			table.SetHeader(keyOrder)

			for _, v := range allEvents {
				kv := eventToKV(v)
				tableRow := []string{}
				for _, k := range keyOrder {
					v, _ := kv[k]
					tableRow = append(tableRow, v)
				}
				table.Append(tableRow)
			}

			table.Render()

			fmt.Print(buf.String())
		} else {
			return printEvent(allEvents, format, keyOrder)
		}

		return nil
	},
}

func init() {
	initListCmdFlags()
	RootCmd.AddCommand(ListCmd)
}

func initListCmdFlags() {
	ListCmd.Flags().StringP("target-type", "", "", "filter events by a target type")
	ListCmd.Flags().StringP("target-id", "", "", "filter events by a target ID")
	ListCmd.Flags().StringP("initiator-id", "", "", "filter events by an initiator ID")
	ListCmd.Flags().StringP("initiator-name", "", "", "filter events by an initiator name")
	ListCmd.Flags().StringP("action", "", "", "filter events by an action")
	ListCmd.Flags().StringP("outcome", "", "", "filter events by an outcome")
	ListCmd.Flags().StringP("source", "", "", "filter events by a source")
	ListCmd.Flags().StringP("time", "", "", "filter events by time")
	ListCmd.Flags().StringP("time-start", "", "", "filter events from time")
	ListCmd.Flags().StringP("time-end", "", "", "filter events till time")
	ListCmd.Flags().IntP("limit", "l", 0, "limit an amount of events in output")
	ListCmd.Flags().StringSliceP("sort", "s", []string{}, `supported sort keys include time, observer_type, target_type, target_id, initiator_type, initiator_id, outcome and action
each sort key may also include a direction suffix
supported directions are ":asc" for ascending and ":desc" for descending
can be specified multiple times`)
}
