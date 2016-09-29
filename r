#!/bin/bash
go install -v
if [ $? -eq "0" ]
then
	boringstreamer -v
	exit
fi
echo go install failed
