package agora

import (
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
)

// newProgressWriter sets up a go-pretty progress.Writer rendering to the
// terminal, styled the same way agora-uploader's CLIProgressHandler is.
func newProgressWriter(nrExpectedTrackers int) progress.Writer {
	pw := progress.NewWriter()
	pw.SetAutoStop(false)
	pw.SetTrackerLength(40)
	pw.SetMessageLength(30)
	pw.SetNumTrackersExpected(nrExpectedTrackers)
	pw.SetSortBy(progress.SortByNone)
	pw.SetStyle(progress.StyleDefault)
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetUpdateFrequency(time.Millisecond * 100)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.PercentFormat = "%4.1f%%"
	pw.Style().Visibility.ETA = false
	pw.Style().Visibility.ETAOverall = false
	pw.Style().Visibility.Percentage = true
	pw.Style().Visibility.Speed = true
	pw.Style().Visibility.SpeedOverall = false
	pw.Style().Visibility.Time = false
	pw.Style().Visibility.TrackerOverall = true
	pw.Style().Visibility.Value = false
	pw.Style().Visibility.Pinned = false

	go pw.Render()
	return pw
}
