package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"agora-downloader/agora"
	"agora-downloader/log"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
)

var appVersion = "0.0.1"
var buildTime = "N.A."
var gitCommit = "N.A."
var gitRef = "N.A."

func credentials() (string, string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Agora Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return "", "", err
	}

	fmt.Print("Agora Password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", "", err
	}
	fmt.Println()

	password := string(bytePassword)
	return strings.TrimSpace(username), strings.TrimSpace(password), nil
}

func Download(c *cli.Context) error {
	idFlags := []string{"folder-id", "exam-id", "series-id", "dataset-id"}
	nrSet := 0
	for _, name := range idFlags {
		if c.IsSet(name) {
			nrSet++
		}
	}
	if nrSet != 1 {
		logrus.Fatal("Error: exactly one of --folder-id, --exam-id, --series-id or --dataset-id must be provided.")
	}

	apiKey := c.String("api-key")
	username, password := "", ""
	if apiKey == "" {
		var err error
		username, password, err = credentials()
		if err != nil {
			logrus.Fatal("Error: could not read credentials: ", err)
		}
	}

	opts := agora.Options{
		URL:         c.String("url"),
		ApiKey:      apiKey,
		Username:    username,
		Password:    password,
		VerifyCert:  !c.Bool("no-check-certificate"),
		FolderID:    -1,
		ExamID:      -1,
		SeriesID:    -1,
		DatasetID:   -1,
		MaxLevel:    c.Int("level"),
		Flat:        c.Bool("flat"),
		OutputDir:   c.String("output"),
		Concurrency: c.Int("concurrency"),
	}
	if c.IsSet("folder-id") {
		opts.FolderID = c.Int("folder-id")
	}
	if c.IsSet("exam-id") {
		opts.ExamID = c.Int("exam-id")
	}
	if c.IsSet("series-id") {
		opts.SeriesID = c.Int("series-id")
	}
	if c.IsSet("dataset-id") {
		opts.DatasetID = c.Int("dataset-id")
	}

	if err := agora.Download(opts); err != nil {
		logrus.Fatal(err)
	}
	return nil
}

func main() {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:     "url",
			Aliases:  []string{"u"},
			Value:    "",
			Usage:    "The URL to the Agora server",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "api-key",
			Aliases: []string{"k"},
			Value:   "",
			Usage:   "The Agora API key used for authentication",
			EnvVars: []string{"AGORA_API_KEY"},
		},
		&cli.IntFlag{
			Name:  "folder-id",
			Usage: "The ID of the folder to download (downloads every exam/series/dataset directly in it, and its subfolders too - see --level)",
		},
		&cli.IntFlag{
			Name:  "exam-id",
			Usage: "The ID of the exam to download",
		},
		&cli.IntFlag{
			Name:  "series-id",
			Usage: "The ID of the series to download",
		},
		&cli.IntFlag{
			Name:  "dataset-id",
			Usage: "The ID of the dataset to download",
		},
		&cli.IntFlag{
			Name:    "level",
			Aliases: []string{"L"},
			Value:   -1,
			Usage:   "When downloading a folder, how many levels of subfolders to descend into (0 = only the folder's own items, no subfolders; N = descend N levels). Omit for unlimited depth (the default)",
		},
		&cli.BoolFlag{
			Name:  "flat",
			Usage: "Download all files directly into --output with no folder/exam/series/dataset subdirectories. Files with the same name will overwrite each other - use with care",
		},
		&cli.StringFlag{
			Name:     "output",
			Aliases:  []string{"o"},
			Value:    ".",
			Usage:    "The directory to download the data into",
			Required: true,
		},
		&cli.IntFlag{
			Name:    "concurrency",
			Aliases: []string{"j"},
			Value:   4,
			Usage:   "The number of files to download in parallel",
		},
		&cli.BoolFlag{
			Name:  "no-check-certificate",
			Usage: "Don't check the server certificate",
		},
	}

	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Printf("%s version %s\n", c.App.Name, c.App.Version)
		fmt.Printf("\nbuild time: %s\n", buildTime)
		fmt.Printf("git commit: %s\n", gitCommit)
		fmt.Printf("git ref: %s\n", gitRef)
	}

	app := &cli.App{}
	app.Name = "agora-downloader"
	app.Usage = "for downloading data from Agora"
	app.Version = appVersion
	app.Authors = []*cli.Author{
		{
			Name:  "Martin Buehrer",
			Email: "martin.buehrer@gyrotools.com",
		},
	}
	app.Flags = flags
	app.Action = Download
	log.ConfigureLogging(app)

	err := app.Run(os.Args)
	if err != nil {
		logrus.Fatal(err)
	}
}
