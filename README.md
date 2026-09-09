# agora-downloader
helper tool for downloading data from Agora

## Download
The latest release of the agora-downloader can be found [here](https://github.com/GyroTools/agora-downloader/releases/latest/). Please make sure you choose the correct platform.

## Usage
The agora-downloader downloads a folder, exam, series or dataset from the command line with the following syntax:

```
     agora-downloader --url <agora_server_url> --output <output_directory> <one of --folder-id/--exam-id/--series-id/--dataset-id> <options>
```

Downloaded data is organized on disk by exam/series (and, for a multi-file dataset, by dataset), mirroring how the data is organized in Agora - see [Directory layout](#directory-layout) below.

```
OPTIONS:
   -u, --url                 The URL of the Agora server
   -o, --output              The directory to download the data into (default: ".")
   --folder-id               The ID of the folder to download (downloads every exam/series/dataset directly in it, and its subfolders too - see --level)
   --exam-id                 The ID of the exam to download
   --series-id               The ID of the series to download
   --dataset-id              The ID of the dataset to download
   -L, --level                When downloading a folder, how many levels of subfolders to descend into (0 = only the folder's own items, no subfolders; N = descend N levels). Omit for unlimited depth (the default)
   --flat                    Download all files directly into --output with no folder/exam/series/dataset subdirectories. Files with the same name will overwrite each other - use with care
   -k, --api-key             The Agora API key used for authentication (or set AGORA_API_KEY; prompts for username/password if neither is given)
   -j, --concurrency         The number of files to download in parallel (default: 4)
   --no-check-certificate    Don't check the server certificate
   --help                    show help (default: false)
```

Exactly one of `--folder-id`, `--exam-id`, `--series-id` or `--dataset-id` must be given.

### Examples

1. Download an entire folder, including all its subfolders (username and password are prompted on the command line)
     ```
          agora-downloader --url https://my-agora.gyrotools.com --folder-id 13 --output /data/download
     ```

2. Download a single exam and use the Agora api-key for authentication
     ```
          agora-downloader -u https://my-agora.gyrotools.com --exam-id 456 -o /data/download -k 8be8b7bd-5007-4af9-95fa-4c491566d40a
     ```

3. Download a folder, but only its own items - don't descend into subfolders
     ```
          agora-downloader --url https://my-agora.gyrotools.com --folder-id 13 --output /data/download --level 0
     ```

4. Download a folder two levels of subfolders deep
     ```
          agora-downloader --url https://my-agora.gyrotools.com --folder-id 13 --output /data/download --level 2
     ```

5. Download a single series, dumping its files directly into --output with no exam/series subdirectories
     ```
          agora-downloader --url https://my-agora.gyrotools.com --series-id 789 --output /data/download --flat
     ```

6. Skip the verification of the server's ssl certificate (e.g. when using a self-signed certificate)
     ```
          agora-downloader --url https://my-agora.gyrotools.com --exam-id 456 --output /data/download --no-check-certificate
     ```

## Directory layout
- Exam: `<output>/<exam name>/<series name>/<file>`, and `<output>/<exam name>/<file>` for datasets attached directly to the exam (not via a series).
- Series (given directly, or a folder's direct item): `<output>/<series name>/<file>`.
- Dataset with more than one file (given directly, or a folder's direct item): `<output>/<dataset name>/<file>`. A dataset with a single file is placed directly in its parent directory instead - a lone file doesn't need a folder of its own.
- Folder: `<output>/<folder name>/...`, recursing into subfolders the same way (see `--level` to cap the depth, or `--flat` to disable all of this and dump everything directly into `--output`).

A file's own name can itself contain a subfolder (e.g. some DICOM exports use names like `DICOM/IM_0001`) - that part of the layout comes from Agora, not from this tool, and is unaffected by `--flat`.

Re-running a download skips files that already exist with a matching size and checksum, so an interrupted download can simply be resumed by running the same command again.
