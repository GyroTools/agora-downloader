package agora

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file drives Download() against a local httptest server that mimics
// just enough of the Agora v2 REST API to exercise the full folder -> exam
// -> series -> dataset -> datafile traversal, directory layout, and
// skip-existing logic without needing a real Agora server.

type mockDatafile struct {
	ID       int
	Name     string
	Contents string
}

func (m mockDatafile) json() string {
	sum := sha1.Sum([]byte(m.Contents))
	return fmt.Sprintf(`{"id":%d,"original_filename":%q,"size":%d,"sha1":%q}`,
		m.ID, m.Name, len(m.Contents), hex.EncodeToString(sum[:]))
}

func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	datafiles := map[int]mockDatafile{
		1001: {1001, "examfile.txt", "exam-via-series content"},
		1002: {1002, "examdirect.txt", "exam direct dataset content"},
		1003: {1003, "seriesfile.txt", "folder-direct series content"},
		1004: {1004, "datasetfile.txt", "folder-direct dataset content"},
		1005: {1005, "subfolderfile.txt", "subfolder exam content"},
		// original_filename embedding a subfolder, as real DICOM exports do
		// (e.g. "DICOM/IM_0282") - regression test for the bug where the
		// "DICOM" intermediate directory was never created before writing.
		1006: {1006, "DICOM/IM_0001", "dicom instance content"},
		// Two levels deep (folder 1 -> folder 2 -> folder 3), for testing
		// --level's numeric cap rather than just the 0-vs-unlimited case.
		1007: {1007, "subsubfolderfile.txt", "sub-subfolder exam content"},
	}

	datasetFiles := map[int][]int{
		103: {1001},
		102: {1002},
		201: {1003},
		300: {1004, 1006},
		111: {1005},
		112: {1007},
	}
	datasetNames := map[int]string{
		103: "Ds103", 102: "Ds102", 201: "Ds201", 300: "Ds300-Direct", 111: "Ds111", 112: "Ds112",
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/current/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"institution":"test"}`))
	})
	mux.HandleFunc("/api/v1/version/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.0"}`))
	})

	mux.HandleFunc("/api/v2/folder/1/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":1,"name":"TestFolder","project":3}`))
	})
	mux.HandleFunc("/api/v2/folder/1/items/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id":1,"folder":1,"content_type":"exam","object_id":100,"content_object":{"id":100,"name":"TestExam"}},
			{"id":2,"folder":1,"content_type":"serie","object_id":200,"content_object":{"id":200,"name":"TestSeries200","exam":null}},
			{"id":3,"folder":1,"content_type":"dataset","object_id":300,"content_object":{"id":300,"name":"Ds300-Direct","total_size":0}},
			{"id":4,"folder":1,"content_type":"folder","object_id":2,"content_object":{"id":2,"name":"TestSubfolder","project":3}}
		]`))
	})
	mux.HandleFunc("/api/v2/folder/2/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":2,"name":"TestSubfolder","project":3}`))
	})
	mux.HandleFunc("/api/v2/folder/2/items/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id":5,"folder":2,"content_type":"exam","object_id":110,"content_object":{"id":110,"name":"SubExam"}},
			{"id":7,"folder":2,"content_type":"folder","object_id":3,"content_object":{"id":3,"name":"TestSubSubfolder","project":3}}
		]`))
	})
	mux.HandleFunc("/api/v2/folder/3/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":3,"name":"TestSubSubfolder","project":3}`))
	})
	mux.HandleFunc("/api/v2/folder/3/items/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id":6,"folder":3,"content_type":"exam","object_id":120,"content_object":{"id":120,"name":"SubSubExam"}}
		]`))
	})

	mux.HandleFunc("/api/v2/exam/100/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":100,"name":"TestExam"}`))
	})
	mux.HandleFunc("/api/v2/exam/100/series/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":101,"name":"Series101","exam":100}]`))
	})
	mux.HandleFunc("/api/v2/exam/100/datasets/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":102,"name":"Ds102","total_size":0}]`))
	})

	mux.HandleFunc("/api/v2/exam/110/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":110,"name":"SubExam"}`))
	})
	mux.HandleFunc("/api/v2/exam/110/series/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/api/v2/exam/110/datasets/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":111,"name":"Ds111","total_size":0}]`))
	})

	mux.HandleFunc("/api/v2/exam/120/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":120,"name":"SubSubExam"}`))
	})
	mux.HandleFunc("/api/v2/exam/120/series/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/api/v2/exam/120/datasets/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":112,"name":"Ds112","total_size":0}]`))
	})

	mux.HandleFunc("/api/v2/series/101/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":101,"name":"Series101","exam":100}`))
	})
	mux.HandleFunc("/api/v2/series/101/datasets/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":103,"name":"Ds103","total_size":0}]`))
	})
	mux.HandleFunc("/api/v2/series/200/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":200,"name":"TestSeries200","exam":null}`))
	})
	mux.HandleFunc("/api/v2/series/200/datasets/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":201,"name":"Ds201","total_size":0}]`))
	})

	// Generic dataset/datafiles handler for every dataset id referenced above.
	// (A dedicated "/api/v2/dataset/300/" handler would be wrong here: since
	// it ends in "/", http.ServeMux treats it as a subtree match and it would
	// also swallow "/api/v2/dataset/300/datafiles/".)
	mux.HandleFunc("/api/v2/dataset/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v2/dataset/")
		parts := strings.Split(strings.TrimSuffix(rest, "/"), "/")
		var id int
		fmt.Sscanf(parts[0], "%d", &id)

		if len(parts) == 1 {
			w.Write([]byte(fmt.Sprintf(`{"id":%d,"name":%q,"total_size":0}`, id, datasetNames[id])))
			return
		}
		if len(parts) == 2 && parts[1] == "datafiles" {
			var out []string
			for _, dfID := range datasetFiles[id] {
				out = append(out, datafiles[dfID].json())
			}
			w.Write([]byte("[" + strings.Join(out, ",") + "]"))
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/api/v1/datafile/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/datafile/")
		parts := strings.Split(strings.TrimSuffix(rest, "/"), "/")
		var id int
		fmt.Sscanf(parts[0], "%d", &id)
		if len(parts) == 2 && parts[1] == "download" {
			df, ok := datafiles[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(df.Contents))
			return
		}
		http.NotFound(w, r)
	})

	return httptest.NewServer(mux)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %s", path, err)
	}
	return string(b)
}

func TestDownloadFolderLevel0(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: 1, ExamID: -1, SeriesID: -1, DatasetID: -1,
		MaxLevel: 0, OutputDir: outDir, Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	cases := map[string]string{
		filepath.Join(outDir, "TestFolder", "TestExam", "Series101", "examfile.txt"): "exam-via-series content",
		filepath.Join(outDir, "TestFolder", "TestExam", "examdirect.txt"):            "exam direct dataset content",
		filepath.Join(outDir, "TestFolder", "TestSeries200", "seriesfile.txt"):       "folder-direct series content",
		filepath.Join(outDir, "TestFolder", "Ds300-Direct", "datasetfile.txt"):       "folder-direct dataset content",
		filepath.Join(outDir, "TestFolder", "Ds300-Direct", "DICOM", "IM_0001"):      "dicom instance content",
	}
	for path, want := range cases {
		got := readFile(t, path)
		if got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}

	// level 0: no subfolder content must be present
	subPath := filepath.Join(outDir, "TestFolder", "TestSubfolder")
	if _, err := os.Stat(subPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to not exist at --level 0", subPath)
	}
}

func TestDownloadFolderLevel1(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: 1, ExamID: -1, SeriesID: -1, DatasetID: -1,
		MaxLevel: 1, OutputDir: outDir, Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	// One level of subfolders is allowed: TestSubfolder's own content is present...
	subFile := filepath.Join(outDir, "TestFolder", "TestSubfolder", "SubExam", "subfolderfile.txt")
	if got := readFile(t, subFile); got != "subfolder exam content" {
		t.Errorf("%s: got %q, want %q", subFile, got, "subfolder exam content")
	}

	// ...but a second level (TestSubfolder's own subfolder, TestSubSubfolder)
	// must NOT be descended into.
	subSubPath := filepath.Join(outDir, "TestFolder", "TestSubfolder", "TestSubSubfolder")
	if _, err := os.Stat(subSubPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to not exist at --level 1", subSubPath)
	}
}

func TestDownloadFolderUnlimitedLevel(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: 1, ExamID: -1, SeriesID: -1, DatasetID: -1,
		MaxLevel: -1, OutputDir: outDir, Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	subFile := filepath.Join(outDir, "TestFolder", "TestSubfolder", "SubExam", "subfolderfile.txt")
	got := readFile(t, subFile)
	if got != "subfolder exam content" {
		t.Errorf("%s: got %q, want %q", subFile, got, "subfolder exam content")
	}

	// unlimited depth: the second-level subfolder's content must also be present.
	subSubFile := filepath.Join(outDir, "TestFolder", "TestSubfolder", "TestSubSubfolder", "SubSubExam", "subsubfolderfile.txt")
	if got2 := readFile(t, subSubFile); got2 != "sub-subfolder exam content" {
		t.Errorf("%s: got %q, want %q", subSubFile, got2, "sub-subfolder exam content")
	}
}

func TestDownloadFlat(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: 1, ExamID: -1, SeriesID: -1, DatasetID: -1,
		MaxLevel: -1, Flat: true, OutputDir: outDir, Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	// --flat: none of this tool's own folder/exam/series/dataset
	// subdirectories are created - every file lands directly in outDir.
	cases := map[string]string{
		filepath.Join(outDir, "examfile.txt"):         "exam-via-series content",
		filepath.Join(outDir, "examdirect.txt"):       "exam direct dataset content",
		filepath.Join(outDir, "seriesfile.txt"):       "folder-direct series content",
		filepath.Join(outDir, "datasetfile.txt"):      "folder-direct dataset content",
		filepath.Join(outDir, "subfolderfile.txt"):    "subfolder exam content",
		filepath.Join(outDir, "subsubfolderfile.txt"): "sub-subfolder exam content",
		// The "DICOM/" prefix here comes from the datafile's own
		// OriginalFilename (handled by the connector), not from this
		// tool's own layout decisions - --flat does not affect it.
		filepath.Join(outDir, "DICOM", "IM_0001"): "dicom instance content",
	}
	for path, want := range cases {
		got := readFile(t, path)
		if got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}

	for _, dir := range []string{"TestFolder", "TestExam", "Series101", "TestSeries200", "Ds300-Direct", "TestSubfolder"} {
		if _, err := os.Stat(filepath.Join(outDir, dir)); !os.IsNotExist(err) {
			t.Errorf("expected no %s subdirectory with --flat", dir)
		}
	}
}

func TestDownloadExamDirect(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: -1, ExamID: 100, SeriesID: -1, DatasetID: -1,
		OutputDir: outDir, Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	got := readFile(t, filepath.Join(outDir, "TestExam", "Series101", "examfile.txt"))
	if got != "exam-via-series content" {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestDownloadDatasetDirectSingleFile(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	// Dataset 102 has a single file - it must land directly in outDir, with
	// no "Ds102" subfolder, unlike a multi-file dataset (see
	// TestDownloadSkipsExistingFile, which uses dataset 300 and does expect
	// a subfolder since it has 2 files).
	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: -1, ExamID: -1, SeriesID: -1, DatasetID: 102,
		OutputDir: outDir, Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	got := readFile(t, filepath.Join(outDir, "examdirect.txt"))
	want := "exam direct dataset content"
	if got != want {
		t.Errorf("unexpected content: %q", got)
	}

	if _, err := os.Stat(filepath.Join(outDir, "Ds102")); !os.IsNotExist(err) {
		t.Errorf("expected no Ds102 subfolder for a single-file dataset")
	}
}

func TestDownloadSeriesDirect(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: -1, ExamID: -1, SeriesID: 200, DatasetID: -1,
		OutputDir: outDir, Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("Download failed: %s", err)
	}

	// A series' datasets must not get their own extra subfolder - files
	// land directly under <series.Name>/.
	got := readFile(t, filepath.Join(outDir, "TestSeries200", "seriesfile.txt"))
	want := "folder-direct series content"
	if got != want {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestDownloadSkipsExistingFile(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	opts := Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: -1, ExamID: -1, SeriesID: -1, DatasetID: 300,
		OutputDir: outDir, Concurrency: 1,
	}

	if err := Download(opts); err != nil {
		t.Fatalf("first download failed: %s", err)
	}
	path := filepath.Join(outDir, "Ds300-Direct", "datasetfile.txt")
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file missing after first download: %s", err)
	}

	// Re-running should skip (not error, not truncate) the already-downloaded file.
	if err := Download(opts); err != nil {
		t.Fatalf("second download failed: %s", err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file missing after second download: %s", err)
	}
	if info1.ModTime() != info2.ModTime() {
		t.Errorf("expected file to be untouched on re-download (skip), but mtime changed")
	}
}

func TestDownloadRequiresExactlyOneID(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	outDir, err := os.MkdirTemp("", "agora_dl_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outDir)

	err = Download(Options{
		URL: srv.URL, ApiKey: "dummy", VerifyCert: false,
		FolderID: -1, ExamID: -1, SeriesID: -1, DatasetID: -1,
		OutputDir: outDir, Concurrency: 1,
	})
	if err == nil {
		t.Errorf("expected an error when no id is set")
	}
}
