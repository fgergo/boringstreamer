@echo off
go install -v
if %errorlevel% gtr 0 (
	echo go install failed
	exit /b %errorlevel%
)
start boringstreamer -v
