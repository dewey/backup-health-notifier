package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/mattevans/postmark-go"
	"github.com/peterbourgon/ff/v3"
)

// executeBackupScript runs the database backup shell script and checks the exit code
func executeBackupScript(passFilePath string, configFilePath string, scriptPath string) error {
	var (
		ee *exec.ExitError
		pe *os.PathError
	)
	if scriptPath == "" {
		return errors.New("script path is empty")
	}
	if configFilePath == "" {
		return errors.New("config file path is empty")
	}
	cmd := exec.Command("bash", "-c", "source "+configFilePath)
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("/bin/sh", scriptPath)
	cmd.Env = os.Environ()

	cmd.Env = append(cmd.Env, fmt.Sprintf("PGPASSFILE=%s", passFilePath))
	err := cmd.Run()
	if errors.As(err, &ee) {
		return fmt.Errorf("exit code error: %d", ee.ExitCode()) // ran, but non-zero exit code

	} else if errors.As(err, &pe) {
		return fmt.Errorf("os.PathError: %v", pe) // "no such file ...", "permission denied" etc.

	} else if err != nil {
		return err

	} else {
		return nil
	}
}

type HistoryComparisonFiles struct {
	FileName            string `json:"file_name"`
	Status              string `json:"status"`
	FileSizeNew         int64  `json:"file_size_new"`
	FileSizeOld         int64  `json:"file_size_old"`
	Difference          int64  `json:"difference"`
	DifferenceHumanized string `json:"difference_humanized"`
}
type HistoryComparison struct {
	BackupName           string                   `json:"backup_name"`
	BackupComparisonName string                   `json:"backup_comparison_name"`
	ComparedFiles        []HistoryComparisonFiles `json:"compared_files"`
}

// checkBackupHistory reads the backup directory and checks the backup history, we return if a backup exists and if there's any errors
func checkBackupHistory(backupPath string) ([]HistoryComparison, bool, error) {
	// Read directory contents
	parentBackupPath := backupPath
	files, err := os.ReadDir(parentBackupPath)
	if err != nil {
		return nil, false, err
	}

	// Create a slice to hold file info and names
	var fileInfos []os.DirEntry

	// Filter directories and gather info
	for _, file := range files {
		if file.IsDir() {
			fileInfos = append(fileInfos, file)
		}
	}

	// Sort fileInfos by directory name (descending)
	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].Name() > fileInfos[j].Name()
	})

	// If there are no backups yet, return false
	if len(fileInfos) == 0 {
		return nil, false, nil
	}
	// We can't compare to previous one if there's only one
	if len(fileInfos) == 1 {
		return nil, true, nil
	}

	type backupMeta struct {
		backupName string
		fileSizes  map[string]int64
	}

	var previousBackups []backupMeta
	for i, fileInfo := range fileInfos {
		if i < 2 {
			files, err := os.ReadDir(path.Join(parentBackupPath, fileInfo.Name()))
			if err != nil {
				return nil, true, err
			}

			// Create a map to hold filename and filesize
			fileSizeMap := make(map[string]int64)

			// Iterate over directory entries
			for _, file := range files {
				if !file.IsDir() {
					info, err := file.Info()
					if err != nil {
						fmt.Println("Error getting file info:", err)
						continue
					}
					fileSizeMap[file.Name()] = info.Size()
				}
			}
			previousBackups = append(previousBackups, backupMeta{
				backupName: fileInfo.Name(),
				fileSizes:  fileSizeMap,
			})
		}
	}

	var historyComparisons []HistoryComparison
	for i := 0; i < len(previousBackups)-1; i++ {
		current := previousBackups[i].fileSizes
		next := previousBackups[i+1].fileSizes

		//fmt.Printf("Comparing entry %s with entry %s:\n", previousBackups[i].backupName, previousBackups[i+1].backupName)
		diff := HistoryComparison{
			BackupName:           previousBackups[i].backupName,
			BackupComparisonName: previousBackups[i+1].backupName,
		}
		// Check keys in the current map
		for key, currentValue := range current {
			if nextValue, exists := next[key]; exists {
				//fmt.Printf("Key '%s' exists in both. Difference: %d\n", key, nextValue-currentValue)
				hcf := HistoryComparisonFiles{
					FileName:    key,
					Status:      "existing",
					FileSizeNew: nextValue,
					FileSizeOld: currentValue,
					Difference:  nextValue - currentValue,
				}
				var difference string
				if hcf.Difference > 0 {
					difference = humanize.Bytes(uint64(hcf.Difference))
				} else {
					difference = fmt.Sprintf("-%s", humanize.Bytes(uint64(hcf.Difference*-1)))
				}
				if hcf.Difference > 0 {
					hcf.DifferenceHumanized = fmt.Sprintf("Difference: %s (Old: %s, New: %s)", difference, humanize.Bytes(uint64(hcf.FileSizeOld)), humanize.Bytes(uint64(hcf.FileSizeNew)))
				} else {
					hcf.DifferenceHumanized = fmt.Sprintf("Difference: None (Current: %s)", humanize.Bytes(uint64(hcf.FileSizeOld)))
				}

				diff.ComparedFiles = append(diff.ComparedFiles, hcf)

			} else {
				//fmt.Printf("Key '%s' was deleted.\n", key)
				diff.ComparedFiles = append(diff.ComparedFiles, HistoryComparisonFiles{
					FileName: key,
					Status:   "deleted",
				})
			}
		}

		// Check for new keys in the next map
		for key := range next {
			if _, exists := current[key]; !exists {
				//fmt.Printf("Key '%s' is new.\n", key)
				diff.ComparedFiles = append(diff.ComparedFiles, HistoryComparisonFiles{
					FileName: key,
					Status:   "new",
				})
			}
		}
		historyComparisons = append(historyComparisons, diff)
	}
	return historyComparisons, true, nil
}

// printBackupHistory prints the backup history, it ignores everything below the threshold in bytes
func printBackupHistory(history []HistoryComparison, ignoreThreshold int64) {
	for _, comparison := range history {
		fmt.Printf("Comparison between %s and %s:\n", comparison.BackupName, comparison.BackupComparisonName)
		for _, file := range comparison.ComparedFiles {
			fmt.Println("-------------------------------------------------")
			fmt.Println("File:", file.FileName)
			if file.Status != "existing" {
				fmt.Println("Status:", file.Status)
			}
			if file.Difference != 0 {
				var differenceUnsigned int64
				if file.Difference > 0 {
					differenceUnsigned = file.Difference
				} else {
					differenceUnsigned = file.Difference * -1
				}
				if differenceUnsigned > ignoreThreshold {
					fmt.Println(file.DifferenceHumanized)
				}
				continue
			}
			fmt.Println(file.DifferenceHumanized)
		}
	}
}

// sendEmail sends an email with the backup history
func sendEmail(client *postmark.Client, history []HistoryComparison) error {
	var subjectBackupName string
	for _, comparison := range history {
		subjectBackupName = comparison.BackupName
		break
	}
	emailReq := &postmark.Email{
		From:       "monitoring@notmyhostna.me",
		To:         "mail@notmyhostna.me",
		TemplateID: 36644330,
		TemplateModel: map[string]interface{}{
			"history":     history,
			"backup_name": subjectBackupName,
		},
	}

	_, response, err := client.Email.Send(emailReq)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", response.StatusCode)
	}
	return nil
}

func main() {
	var l log.Logger
	l = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	l = log.With(l, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)
	fs := flag.NewFlagSet("backup-health-notifier", flag.ContinueOnError)
	var (
		postmarkToken    = fs.String("postmark-token", "", "The postmarkapp.com api token")
		backupPath       = fs.String("backup-path", "", "The absolute path to the Postgres backup location")
		configFilePath   = fs.String("config-file-path", "", "The path to the config file used in the Postgres backup script")
		passFilePath     = fs.String("pass-file-path", "", "The path to the Postgres pass file")
		backupScriptPath = fs.String("backup-script-path", "", "The path to the backup script that should be executed")
	)

	if err := ff.Parse(fs, os.Args[1:],
		ff.WithEnvVars(),
	); err != nil {
		level.Error(l).Log("msg", "error parsing flags", "err", err)
		return
	}

	if *configFilePath == "" || *passFilePath == "" || *backupScriptPath == "" {
		level.Error(l).Log("msg", "missing backup environment values for config-file-path, backup-script-path or pass-file-path.")
		return
	}

	if *postmarkToken == "" {
		level.Error(l).Log("msg", "missing postmark token")
		return
	}
	if *backupPath == "" {
		level.Error(l).Log("msg", "missing backup path")
		return
	}
	client := postmark.NewClient(
		postmark.WithClient(&http.Client{
			Transport: &postmark.AuthTransport{Token: *postmarkToken},
		}),
	)
	if err := executeBackupScript(*passFilePath, *configFilePath, *backupScriptPath); err != nil {
		level.Error(l).Log("msg", "error running backup", "err", err)
		return
	}
	history, exists, err := checkBackupHistory(*backupPath)
	if err != nil {
		level.Error(l).Log("msg", "error checking backup history", "err", err)
		return
	}
	if exists {
		level.Info(l).Log("msg", "backup exists, send out health status message")
		if err := sendEmail(client, history); err != nil {
			level.Error(l).Log("msg", "error sending email", "err", err)
			return
		}
		return
	}
	level.Info(l).Log("msg", "backup does not exist, no health status message sent")
}
