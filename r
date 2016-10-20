#!/bin/bash
echo go install -v
go install -v
if [ $? -eq "0" ]
then
	${PWD##*/} $*
	exit
fi
echo go install failed
