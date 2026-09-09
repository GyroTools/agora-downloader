package agora

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/sirupsen/logrus"

	agoraModels "github.com/GyroTools/gtagora-connector-go/agora/models"
)

// Options configures a single Download call. Exactly one of FolderID,
// ExamID, SeriesID or DatasetID must be set (a value >= 0).
type Options struct {
	URL        string
	ApiKey     string
	Username   string
	Password   string
	VerifyCert bool

	FolderID  int
	ExamID    int
	SeriesID  int
	DatasetID int

	// MaxLevel controls how many levels of subfolders a folder download
	// descends into: -1 means unlimited depth (the default), 0 means don't
	// descend into subfolders at all (only the requested folder's own
	// items), and N descends N levels of subfolders.
	MaxLevel int

	// Flat, if true, skips every folder/exam/series/dataset subdirectory
	// this tool would otherwise create - all files are downloaded directly
	// into OutputDir. This does not affect subfolders that come from a
	// datafile's own OriginalFilename (e.g. "DICOM/IM_0001"); that layout
	// comes from the server, not from this tool's own organizing. Files
	// with the same name will overwrite each other.
	Flat bool

	OutputDir   string
	Concurrency int
}

// downloadJob is a single datafile queued for download, together with the
// directory it should land in.
type downloadJob struct {
	Datafile agoraModels.Datafile
	DestDir  string
}

var illegalCharsReplacer = strings.NewReplacer(
	":", "", "*", "", "\"", "", "<", "", ">", "", "|", "",
)

// sanitize mirrors the reference Python connector's remove_illegal_chars: it
// strips characters that are illegal in file/directory names on common
// filesystems.
func sanitize(name string) string {
	name = illegalCharsReplacer.Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unnamed"
	}
	return name
}

// subDir returns filepath.Join(base, sanitize(name)), or just base
// unchanged when flat is true - the single place every layout decision in
// this file goes through, so --flat disables all of them uniformly.
func subDir(flat bool, base, name string) string {
	if flat {
		return base
	}
	return filepath.Join(base, sanitize(name))
}

// Download resolves opts' input (folder/exam/series/dataset) into a set of
// datafiles and downloads them all, laid out under opts.OutputDir.
func Download(opts Options) error {
	a, err := connect(opts.URL, opts.ApiKey, opts.Username, opts.Password, opts.VerifyCert)
	if err != nil {
		return err
	}

	var jobs []downloadJob

	switch {
	case opts.FolderID >= 0:
		folder, err := a.GetFolder(opts.FolderID)
		if err != nil {
			return fmt.Errorf("folder %d not found: %w", opts.FolderID, err)
		}
		folderDir := subDir(opts.Flat, opts.OutputDir, folder.Name)
		if err := collectFolder(folder, folderDir, opts.MaxLevel, opts.Flat, &jobs); err != nil {
			return err
		}
	case opts.ExamID >= 0:
		exam, err := a.GetStudy(opts.ExamID)
		if err != nil {
			return fmt.Errorf("exam %d not found: %w", opts.ExamID, err)
		}
		if err := collectExam(exam, opts.OutputDir, opts.Flat, &jobs); err != nil {
			return err
		}
	case opts.SeriesID >= 0:
		series, err := a.GetSeries(opts.SeriesID)
		if err != nil {
			return fmt.Errorf("series %d not found: %w", opts.SeriesID, err)
		}
		if err := collectSeries(series, opts.OutputDir, opts.Flat, &jobs); err != nil {
			return err
		}
	case opts.DatasetID >= 0:
		dataset, err := a.GetDataset(opts.DatasetID)
		if err != nil {
			return fmt.Errorf("dataset %d not found: %w", opts.DatasetID, err)
		}
		if err := placeDatasetFiles(dataset, opts.OutputDir, opts.Flat, &jobs); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no folder/exam/series/dataset id given")
	}

	if len(jobs) == 0 {
		logrus.Warn("nothing to download")
		return nil
	}

	return runJobs(jobs, opts.Concurrency)
}

// collectExam appends all of exam's datafiles to jobs, laid out as
// <outDir>/<exam.Name>/<series.Name>/<file> for datafiles reached via a
// series, and <outDir>/<exam.Name>/<file> for datasets attached directly to
// the exam - see placeDatasetFiles for when a dataset gets its own
// subfolder within that.
func collectExam(exam *agoraModels.Study, outDir string, flat bool, jobs *[]downloadJob) error {
	examDir := subDir(flat, outDir, exam.Name)

	series, err := exam.GetSeries()
	if err != nil {
		return fmt.Errorf("cannot list series for exam %d: %w", exam.ID, err)
	}
	for i := range series {
		seriesDir := subDir(flat, examDir, series[i].Name)
		if err := collectDatasetsForSeries(&series[i], seriesDir, flat, jobs); err != nil {
			return err
		}
	}

	datasets, err := exam.GetDirectDatasets()
	if err != nil {
		return fmt.Errorf("cannot list datasets for exam %d: %w", exam.ID, err)
	}
	for i := range datasets {
		if err := placeDatasetFiles(&datasets[i], examDir, flat, jobs); err != nil {
			return err
		}
	}
	return nil
}

// collectSeries appends all of series' datafiles to jobs, laid out under
// <outDir>/<series.Name>/ - see placeDatasetFiles for when a dataset within
// the series gets its own subfolder there.
func collectSeries(series *agoraModels.Series, outDir string, flat bool, jobs *[]downloadJob) error {
	seriesDir := subDir(flat, outDir, series.Name)
	return collectDatasetsForSeries(series, seriesDir, flat, jobs)
}

func collectDatasetsForSeries(series *agoraModels.Series, destDir string, flat bool, jobs *[]downloadJob) error {
	datasets, err := series.GetDatasets()
	if err != nil {
		return fmt.Errorf("cannot list datasets for series %d: %w", series.ID, err)
	}
	for i := range datasets {
		if err := placeDatasetFiles(&datasets[i], destDir, flat, jobs); err != nil {
			return err
		}
	}
	return nil
}

// placeDatasetFiles appends all of dataset's datafiles to jobs. If the
// dataset has more than one file, they're placed under
// <parentDir>/<dataset.Name>/ to keep them together and avoid colliding
// with another dataset's same-named files (e.g. a DICOM series export with
// hundreds of files). A dataset with 0 or 1 files is placed directly in
// parentDir - a lone file doesn't need a folder of its own. flat disables
// the subfolder unconditionally.
func placeDatasetFiles(dataset *agoraModels.Dataset, parentDir string, flat bool, jobs *[]downloadJob) error {
	datafiles, err := dataset.GetDatafiles()
	if err != nil {
		return fmt.Errorf("cannot list datafiles for dataset %d: %w", dataset.ID, err)
	}
	destDir := parentDir
	if !flat && len(datafiles) > 1 {
		destDir = filepath.Join(parentDir, sanitize(dataset.Name))
	}
	for _, df := range datafiles {
		*jobs = append(*jobs, downloadJob{Datafile: df, DestDir: destDir})
	}
	return nil
}

// collectFolder appends the datafiles of every exam/series/dataset that is a
// direct item of folder, and, while remainingLevels allows it, of every
// subfolder too. remainingLevels is -1 for unlimited depth, 0 to not
// descend into subfolders at all, or N to descend N more levels.
func collectFolder(folder *agoraModels.Folder, outDir string, remainingLevels int, flat bool, jobs *[]downloadJob) error {
	exams, err := folder.GetExams()
	if err != nil {
		return fmt.Errorf("cannot list exams for folder %d: %w", folder.ID, err)
	}
	for i := range exams {
		if err := collectExam(&exams[i], outDir, flat, jobs); err != nil {
			return err
		}
	}

	series, err := folder.GetSeries()
	if err != nil {
		return fmt.Errorf("cannot list series for folder %d: %w", folder.ID, err)
	}
	for i := range series {
		if err := collectSeries(&series[i], outDir, flat, jobs); err != nil {
			return err
		}
	}

	datasets, err := folder.GetDatasets()
	if err != nil {
		return fmt.Errorf("cannot list datasets for folder %d: %w", folder.ID, err)
	}
	for i := range datasets {
		if err := placeDatasetFiles(&datasets[i], outDir, flat, jobs); err != nil {
			return err
		}
	}

	if remainingLevels == 0 {
		return nil
	}
	nextLevel := remainingLevels
	if remainingLevels > 0 {
		nextLevel--
	}

	subfolders, err := folder.GetFolders()
	if err != nil {
		return fmt.Errorf("cannot list subfolders for folder %d: %w", folder.ID, err)
	}
	for i := range subfolders {
		subfolderDir := subDir(flat, outDir, subfolders[i].Name)
		if err := collectFolder(&subfolders[i], subfolderDir, nextLevel, flat, jobs); err != nil {
			return err
		}
	}
	return nil
}

// jobResult is the outcome of downloading a single job.
type jobResult struct {
	job     downloadJob
	skipped bool
	err     error
}

// runJobs downloads jobs using a bounded worker pool, showing per-file and
// total progress bars, then prints a summary. It returns an error if any
// file failed to download.
func runJobs(jobs []downloadJob, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}

	var totalSize int64
	for _, j := range jobs {
		totalSize += j.Datafile.Size
	}

	pw := newProgressWriter(len(jobs) + 1)
	totalTracker := &progress.Tracker{Message: "Total", Total: totalSize, Units: progress.UnitsBytes, RemoveOnCompletion: false}
	pw.AppendTracker(totalTracker)

	jobCh := make(chan downloadJob, len(jobs))
	resultCh := make(chan jobResult, len(jobs))
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				downloadOne(job, pw, totalTracker, resultCh)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var nrDownloaded, nrSkipped int
	var failed []jobResult
	for res := range resultCh {
		switch {
		case res.err != nil:
			failed = append(failed, res)
		case res.skipped:
			nrSkipped++
		default:
			nrDownloaded++
		}
	}

	totalTracker.MarkAsDone()
	time.Sleep(150 * time.Millisecond)
	pw.Stop()

	printReport(len(jobs), nrDownloaded, nrSkipped, failed)

	if len(failed) > 0 {
		return fmt.Errorf("%d file(s) failed to download", len(failed))
	}
	return nil
}

// downloadOne downloads a single job's datafile, reporting progress on both
// a per-file tracker and the shared total tracker.
func downloadOne(job downloadJob, pw progress.Writer, totalTracker *progress.Tracker, resultCh chan<- jobResult) {
	tracker := &progress.Tracker{
		Message:            job.Datafile.OriginalFilename,
		Total:              job.Datafile.Size,
		Units:              progress.UnitsBytes,
		RemoveOnCompletion: true,
	}
	pw.AppendTracker(tracker)

	_, skipped, err := job.Datafile.Download(job.DestDir, func(n int64) {
		tracker.Increment(n)
		totalTracker.Increment(n)
	})
	if err != nil {
		tracker.MarkAsErrored()
		resultCh <- jobResult{job: job, err: err}
		return
	}
	if skipped {
		tracker.SetValue(job.Datafile.Size)
		totalTracker.Increment(job.Datafile.Size)
	}
	tracker.MarkAsDone()
	resultCh <- jobResult{job: job, skipped: skipped}
}

func printReport(total, downloaded, skipped int, failed []jobResult) {
	logrus.Info("")
	logrus.Info("Download Result:")
	logrus.Info("-----------------")
	logrus.Infof("Total Files : %d", total)
	logrus.Infof("Downloaded  : %d", downloaded)
	logrus.Infof("Skipped     : %d (already downloaded)", skipped)

	if len(failed) > 0 {
		logrus.Info("")
		logrus.Errorf("%d file(s) failed to download:", len(failed))
		for _, f := range failed {
			logrus.Errorf("  - %s (in %s): %s", f.job.Datafile.OriginalFilename, f.job.DestDir, f.err.Error())
		}
		return
	}

	logrus.Info("")
	logrus.Info("\033[32mAll files were downloaded successfully!\033[0m")
}
