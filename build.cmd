@echo off

if "%1" == "wsl" (
	set GOARCH=amd64
	set GOOS=linux
) else if "%1" == "rpi" (
	set GOARCH=arm64
	set GOOS=linux
) else (
rem ryzen
	set GOARCH=amd64
	set GOOS=windows
)

go build gps.go

pause
