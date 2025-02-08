package main

import (
	"bufio"
	"fmt"
	"github.com/gdamore/tcell/v2"

	"github.com/rivo/tview"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ListeningProcess struct {
	PID              string
	Command          string
	User             string
	FileDesc         string
	Protocol         string
	Address          string
	Port             int
	ExecutablePath   string
	WorkingDirectory string
}

func displayTable() {
	app := tview.NewApplication()
	table := tview.NewTable().SetBorders(true)
	var processes []ListeningProcess

	updateTable := func() {
		processes = getListeningProcesses()

		table.Clear() // Clear the table to refresh
		// Set header row
		col := 0
		table.SetCell(0, col, tview.NewTableCell("Port").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("Address").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("Protocol").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("PID").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("User").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("Command").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("Working Directory").SetSelectable(false))
		col++
		table.SetCell(0, col, tview.NewTableCell("Executable Path").SetSelectable(false))
		col++

		for i, process := range processes {
			col := 0
			table.SetCell(i+1, col, tview.NewTableCell(strconv.Itoa(process.Port)))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.Address))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.Protocol))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.PID))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.User))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.Command))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.WorkingDirectory))
			col++
			table.SetCell(i+1, col, tview.NewTableCell(process.ExecutablePath))
			col++
		}
	}

	// Handle key events
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			selectedRow, _ := table.GetSelection() // Get selected row
			if selectedRow > 0 && selectedRow <= len(processes) {
				selectedProcess := processes[selectedRow-1]
				showConfirmationDialog(app, table, selectedProcess, func() {
					// Kill the process after confirmation
					err := killProcess(selectedProcess.PID)
					if err != nil {
						showErrorDialog(app, table, fmt.Sprintf("Failed to kill process %s: %v", selectedProcess.PID, err))
					} else {
						showInfoDialog(app, table, fmt.Sprintf("Killed process %s (%s)", selectedProcess.PID, selectedProcess.Command))
						updateTable() // Refresh table
					}
				})
			}
		}
		return event
	})

	// Refresh the table every 2 seconds
	go func() {
		for {
			updateTable()
			app.Draw() // Redraw the app
			time.Sleep(2 * time.Second)
		}
	}()

	// Set up the application
	table.SetSelectable(true, false)
	app.SetRoot(table, true)

	if err := app.Run(); err != nil {
		fmt.Println("Error running application:", err)
	}
}

func main() {
	displayTable()
}

type WorkingDirectory = map[string]string

func getWorkingDirectories() WorkingDirectory {
	cmd := exec.Command("lsof", "-a", "-d", "cwd", "-Fn")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error:", err)
		return nil
	}
	res := parseLsOfWorkingDirectoryOutput(string(output))
	return res
}

func parseLsOfWorkingDirectoryOutput(output string) WorkingDirectory {
	var result = WorkingDirectory{}
	var currentPID = ""

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}

		prefix := line[:1]
		value := line[1:]

		switch prefix {
		case "p": // PID
			currentPID = value

		case "n": // Address
			// Split the address into host and port
			result[currentPID] = value
		}
	}

	return result
}

func getListeningProcesses() []ListeningProcess {
	cmd := exec.Command("lsof", "-PiTCP", "-sTCP:LISTEN", "-F", "pcuftDsin")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error:", err)
		return nil
	}
	res := enrichProcesses(parseLsofOutput(string(output)))

	sort.Slice(res, func(i, j int) bool {
		return res[i].Port <= res[j].Port
	})
	return res
}
func parseLsofOutput(output string) []ListeningProcess {
	var processes []ListeningProcess
	var currentProcess ListeningProcess

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}

		prefix := line[:1]
		value := line[1:]

		switch prefix {
		case "p": // PID
			// Append the current process if it's complete
			if currentProcess.PID != "" {
				processes = append(processes, currentProcess)
			}
			// Start a new process
			currentProcess = ListeningProcess{PID: value}
		case "c": // Command
			currentProcess.Command = value
		case "u": // User
			currentProcess.User = value
		case "f": // File descriptor
			currentProcess.FileDesc = value
		case "t": // Protocol
			currentProcess.Protocol = value
		case "n": // Address
			// Split the address into host and port
			parts := strings.Split(value, ":")
			if len(parts) == 2 {
				currentProcess.Address = parts[0]
				var err error
				currentProcess.Port, err = strconv.Atoi(parts[1])
				if err != nil {
					currentProcess.Port = 0
				}
			} else {
				currentProcess.Address = value
				currentProcess.Port = 0
			}
		}
	}

	// Append the last process
	if currentProcess.PID != "" {
		processes = append(processes, currentProcess)
	}

	return processes
}

type PIDInformation struct {
	ExecutablePath   string
	WorkingDirectory string
}

var pidCache = make(map[string]PIDInformation)

func enrichProcesses(processes []ListeningProcess) []ListeningProcess {
	workingDirectories := getWorkingDirectories()

	for i, process := range processes {
		info, found := pidCache[process.PID]
		if !found {
			info = PIDInformation{
				ExecutablePath:   getExecutablePath(process.PID),
				WorkingDirectory: workingDirectories[process.PID],
			}
			pidCache[process.PID] = info
		}
		processes[i].ExecutablePath = info.ExecutablePath
		processes[i].WorkingDirectory = info.WorkingDirectory
	}
	return processes
}

func getExecutablePath(pid string) string {
	cmd := exec.Command("ps", "-p", pid, "-o", "comm=")
	output, err := cmd.Output()
	if err != nil {
		return "N/A"
	}
	return strings.TrimSpace(string(output))
}

func getWorkingDirectory(pid string) string {
	cmd := exec.Command("lsof", "-p", pid)
	output, err := cmd.Output()
	if err != nil {
		return "N/A"
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "cwd") {
			parts := strings.Fields(line)
			if len(parts) > 3 {
				return parts[len(parts)-1]
			}
		}
	}
	return "N/A"
}

func killProcess(pid string) error {
	cmd := exec.Command("kill", "-9", pid)
	return cmd.Run()
}

func showConfirmationDialog(app *tview.Application, table *tview.Table, process ListeningProcess, onConfirm func()) {

	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to kill process %s (%s)?", process.PID, process.Command)).
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Yes" {
				onConfirm()
			}
			app.SetRoot(table, true) // Return to the table view
		})
	app.SetRoot(modal, false)
}

func showErrorDialog(app *tview.Application, table *tview.Table, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			app.SetRoot(table, true)
		})
	app.SetRoot(modal, false)
}

func showInfoDialog(app *tview.Application, table *tview.Table, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			app.SetRoot(table, true)
		})
	app.SetRoot(modal, false)
}
