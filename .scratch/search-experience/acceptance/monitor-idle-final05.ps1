Set-Location 'C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05'
$out='C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.scratch/search-experience/acceptance/load-idle-final05.jsonl'
for($i=0;$i -lt 900;$i++) {
 $cpu=Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor -Filter "Name='_Total'"
 $os=Get-CimInstance Win32_OperatingSystem
 $procs=Get-CimInstance Win32_PerfFormattedData_PerfProc_Process | Where-Object {$_.Name -notin @('_Total','Idle') -and $_.PercentProcessorTime -gt 20} | Select-Object Name,IDProcess,PercentProcessorTime
 @{time=(Get-Date).ToUniversalTime().ToString('o');cpu=$cpu.PercentProcessorTime;freeKB=$os.FreePhysicalMemory;processes=@($procs)} | ConvertTo-Json -Depth 4 -Compress | Add-Content $out
 Start-Sleep -Seconds 5
}
