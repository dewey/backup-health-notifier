package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
)

// runBackup runs the database backup shell script and checks the exit code
func runBackup() error {
	var (
		ee *exec.ExitError
		pe *os.PathError
	)
	cmd := exec.Command("/bin/sh", "backup-success.sh")
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "PGPASSFILE=/root/.pgpass")
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

type historyComparisonFiles struct {
	fileName    string
	status      string // new, deleted, difference
	fileSizeNew int64
	fileSizeOld int64
	difference  int64
}
type historyComparison struct {
	backupName           string
	backupComparisonName string
	comparedFiles        []historyComparisonFiles
}

// checkBackupHistory reads the backup directory and checks the backup history, we return if a backup exists and if there's any errors
func checkBackupHistory() ([]historyComparison, bool, error) {
	// Read directory contents
	parentBackupPath := "backup-directory/postgresql-backups"
	files, err := os.ReadDir(parentBackupPath)
	if err != nil {
		fmt.Println("Error reading directory:", err)
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
		if i <= 2 {
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

	var historyComparisons []historyComparison
	for i := 0; i < len(previousBackups)-1; i++ {
		current := previousBackups[i].fileSizes
		next := previousBackups[i+1].fileSizes

		//fmt.Printf("Comparing entry %s with entry %s:\n", previousBackups[i].backupName, previousBackups[i+1].backupName)
		diff := historyComparison{
			backupName:           previousBackups[i].backupName,
			backupComparisonName: previousBackups[i+1].backupName,
		}
		// Check keys in the current map
		for key, currentValue := range current {
			if nextValue, exists := next[key]; exists {
				//fmt.Printf("Key '%s' exists in both. Difference: %d\n", key, nextValue-currentValue)
				diff.comparedFiles = append(diff.comparedFiles, historyComparisonFiles{
					fileName:    key,
					status:      "difference",
					fileSizeNew: nextValue,
					fileSizeOld: currentValue,
					difference:  nextValue - currentValue,
				})
			} else {
				//fmt.Printf("Key '%s' was deleted.\n", key)
				diff.comparedFiles = append(diff.comparedFiles, historyComparisonFiles{
					fileName: key,
					status:   "deleted",
				})
			}
		}

		// Check for new keys in the next map
		for key := range next {
			if _, exists := current[key]; !exists {
				//fmt.Printf("Key '%s' is new.\n", key)
				diff.comparedFiles = append(diff.comparedFiles, historyComparisonFiles{
					fileName: key,
					status:   "new",
				})
			}
		}
		historyComparisons = append(historyComparisons, diff)
	}
	return historyComparisons, true, nil
}

func main() {
	if err := runBackup(); err != nil {
		fmt.Println(err)
	}
	// Check size of backup file
	history, exists, err := checkBackupHistory()
	if err != nil {
		fmt.Println("Error checking backup history:", err)
	}
	if exists {
		fmt.Println("Backup exists")
		for _, comparison := range history {
			fmt.Printf("Comparison between %s and %s:\n", comparison.backupName, comparison.backupComparisonName)
			for _, file := range comparison.comparedFiles {
				fmt.Printf("File: %s, Status: %s, Difference: %d\n", file.fileName, file.status, file.difference)
			}
		}
	} else {
		fmt.Println("Backup does not exist")
	}
	// Compare size of backup file with previous backup file, check created at timestamp of previous file

	// Run file backup script

}
