go install -v
@echo off
if %errorlevel% gtr 0 (
	echo go install failed
	exit /b %errorlevel%
)

for %%f in (%cd%) do set binary=%%~nxf
@echo on
start %binary% %*
