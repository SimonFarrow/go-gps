@echo off

rem ryzen
set GOARCH=amd64

rem rpi
rem set GOARCH=arm64

rem ryzen
set GOOS=windows

rem wsl/rpi
rem set GOOS=linux

go build gps.go

pause
