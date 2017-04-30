@echo off
setlocal enabledelayedexpansion

set editorlog=%userprofile%\AppData\Local\Unity\Editor\Editor.log
set ahkexe=%programfiles%\AutoHotKey\AutoHotKey.exe

call :findexecutable sleep
set sleepexe=%foundexe%

call :findexecutable tail
set tailexe=%foundexe%

call :findexecutable powershell
set powershellexe=%foundexe%

set counter=0

if [%1]==[] (
    echo Usage: %0 ^<runfirst^> ^<runaftersuccess^>
    exit /b 1
)

set runfirst=%1

if not exist %runfirst% (
    set runfirst=%~dp0%~1
)

if not exist %runfirst% (
    set runfirst=%~dp0unity_%~1.ahk
)

if not exist %runfirst% (
    call :findexecutable %~1
    set runfirst=!foundexe!
)

if not exist %runfirst% (
    exit /b 1
)

call :echomagenta %runfirst% & echo.

if not [%2]==[] (
    set runaftersuccess=%~2

    if not exist !runaftersuccess! (
        set runaftersuccess=%~dp0%~2
    )

    if not exist !runaftersuccess! (
        set runaftersuccess=%~dp0unity_%~2.ahk
    )

    if not exist !runaftersuccess! (
        call :findexecutable %~2
        set runfirst=!foundexe!
    )

    call :echomagenta !runaftersuccess! & echo.
)

call :getlinecount "%editorlog%"
set startline=%linecount%

:runahk
call "%ahkexe%" %runfirst%
call :sleep 100
call "%ahkexe%" %runfirst%
call :sleep 100
call "%ahkexe%" %runfirst%
call :sleep 300

call :getlinecount "%editorlog%"
set /a linecountdiff=!linecount! - !startline!
if !linecountdiff! equ 0 (
    goto :runahk
)

call :getlastmatchline "Hashing ProjectSettings/ProjectSettings\.asset"
set inprogress=%matchline%

set /a inprogressdiff=%inprogress% - %startline%

if %inprogressdiff% geq 0 (
    call :echoyellow "Waiting for Unity to finish..." & echo.
    call :waitfinished

    call :getlinecount "%editorlog%"
)
echo.

call :getlastmatchline "Compilation.succeeded"
set compilesuccess=!matchline!
call :getlastmatchline "Compilation.failed"
set compilefail=!matchline!

set compileresult="success"
if !compilefail! geq !inprogress! (
    if !compilesuccess! geq !compilefail! (
        set compileresult="success"
    ) else (
        set compileresult="failure"
    )
)

if !compilesuccess! geq !inprogress! (
    if !compilefail! geq !compilesuccess! (
        set compileresult="failure"
    ) else (
        set compileresult="success"
    )
)

call :getlastmatchline "\-\-\-\-\-CompilerOutput:\-stdout"
set errorbegin=!matchline!
call :getlastmatchline "\-\-\-\-\-EndCompilerOutput"
set errorend=!matchline!

if !errorbegin! geq !inprogress! (
    call :extracterrors !errorbegin! !errorend!
    echo.
)

if !compileresult!=="success" (
   if defined runaftersuccess (
        call "%ahkexe%" %runaftersuccess%
    )
    call :echogreen "Success."
    exit /b 0
) else (
    call :echored "Failure."
    exit /b 1
)

exit /b 0

:echored
echo | set /p="[31m%~1[0m"
goto:eof

:echogreen
echo | set /p="[32m%~1[0m"
goto:eof

:echoyellow
echo | set /p="[33m%~1[0m"
goto:eof

:echoblue
echo | set /p="[34m%~1[0m"
goto:eof

:echomagenta
echo | set /p="[35m%~1[0m"
goto:eof

:echocyan
echo | set /p="[36m%~1[0m"
goto:eof

:echowhite
echo | set /p="[37m%~1[0m"
goto:eof

:maketailcmd
if defined tailexe (
    set tailcmd=tail -%~1 %~2
) else if defined powershellexe (
    set tailcmd=powershell -c "(Get-Content %~2 -Tail %~1)"
) else (
    set tailcmd=type %~2
)
goto:eof

:sleep
if defined sleepexe (
    sleep -m %1
) else (
    pathping 127.0.0.1 -n -q 1 -p %1 >nul
)
goto:eof

:findexecutable
for /F "delims=" %%i in ('where %~1') do (set foundexe=%%i & goto:eof)
goto:eof


:extracterrors
call :getlinecount "%editorlog%"
set /a tailcount=%linecount% - %1
set /a offsetstart=3
set /a offsetend=%2 - %1
call :maketailcmd %tailcount% %editorlog%
for /f "delims=" %%i in ('%%tailcmd%% ^| findstr /N /R ^^^^') do (
    for /f "tokens=1,* delims=: " %%a in ("%%i") do (
        if %%a equ 1 (
            if !compileresult!=="success" (
                call :echogreen "%%b" & echo.
            ) else (
                call :echored "%%b" & echo.
            )
        )
        if %%a geq %offsetstart% (
            if %%a lss %offsetend% (
                echo %%b
            )
        )
    )
)
goto:eof

:waitfinished
call :getlastmatchline "Registered"
set successline=%matchline%
call :getlastmatchline "\-\-\-\-\-EndCompilerOutput"
set errorline=%matchline%
call :getlastmatchline "(Nothing.changed)"
set nothingchangedline=%matchline%
call :getlinecount "%editorlog%"
set /a nothingchangeddiff=%linecount% - %nothingchangedline%
if %nothingchangeddiff% gtr 1 (
    set /a counter=%counter% + 1
)
if %counter% lss 3 (
    if %successline% lss %startline% (
        if %errorline% lss %startline% (
            goto :waitfinished
        )
    )
)
set counter=0
goto:eof

:getlastmatchline
for /f "delims=" %%a in ('findstr /R /N "%~1" %editorlog%') do set lastline=%%a
for /f "tokens=1 delims=: " %%a in ("%lastline%") do (set matchline=%%a)
goto:eof

:getlinecount
for /f "delims=" %%a in ('findstr /R /N ^^^^ "%~1" ^| find /C ":"') do set linecount=%%a
goto:eof

:getfilesize
for /f "usebackq" %%a in ('%~1') do set filesize=%%~za
goto:eof

:getlastline
for /f "delims=" %%a in ('%~1') do set lastline=%%a
goto:eof

endlocal
