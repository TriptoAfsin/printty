; Printty's Windows installer (Inno Setup 6).
;
; Installs printty.exe to Program Files and runs it as a Windows service, so
; Printty starts with Windows and restarts itself; adds the tray icon for every
; user, a Start menu entry, and the firewall rule (Private networks only).
; Needs administrator rights once, for the service. Upgrading keeps the data
; (users, settings) in C:\ProgramData\Printty; so does uninstalling.
;
; Built by .github/workflows/release.yml:
;   iscc /DAppVersion=0.1.0 /DArch=x64 /DSourceDir=..\..\dist\windows-amd64 packaging\windows\printty.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef Arch
  #define Arch "x64"
#endif
#ifndef SourceDir
  #define SourceDir "..\.."
#endif

[Setup]
AppId={{12B4AD51-E1A2-4D92-B605-83EE40DF3C09}
AppName=Printty
AppVersion={#AppVersion}
AppVerName=Printty {#AppVersion}
AppPublisher=TriptoAfsin
AppPublisherURL=https://github.com/TriptoAfsin/printty
AppSupportURL=https://github.com/TriptoAfsin/printty/issues
AppUpdatesURL=https://github.com/TriptoAfsin/printty/releases
DefaultDirName={autopf}\Printty
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=admin
#if Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#endif
MinVersion=10.0.17763
OutputDir=..\..\dist
OutputBaseFilename=Printty_{#AppVersion}_{#Arch}_setup
SetupIconFile=printty.ico
UninstallDisplayIcon={app}\printty.exe
UninstallDisplayName=Printty
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
; Printty is stopped by [Code] below, not by Restart Manager.
CloseApplications=no
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "{#SourceDir}\printty.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\printty.env.example"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Printty"; Filename: "{app}\printty.exe"; Parameters: "open"; IconFilename: "{app}\printty.exe"; Comment: "Open Printty"; AppUserModelID: "Printty.PrintServer"

[Run]
; Install (or update) the service and start it. Waits until it's listening.
Filename: "{app}\printty.exe"; Parameters: "service install -start"; StatusMsg: "Starting the Printty service..."; Flags: runhidden waituntilterminated
; The tray icon for the person installing (Windows starts it at every sign-in from now on).
Filename: "{app}\printty.exe"; Parameters: "tray -welcome"; Flags: runasoriginaluser runhidden nowait
Filename: "{app}\printty.exe"; Parameters: "open"; Description: "Open Printty to finish setting up"; Flags: runasoriginaluser runhidden nowait postinstall skipifsilent

[UninstallRun]
; Stops and removes the service, its firewall rule and the tray entry.
Filename: "{app}\printty.exe"; Parameters: "service uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "PrinttyService"
; Tray icons in every session, and any print worker still running.
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM printty.exe"; Flags: runhidden waituntilterminated; RunOnceId: "PrinttyKill"

[Code]
{ Upgrading: stop the running service and tray icons so printty.exe can be
  replaced. "service install -start" in [Run] starts it again. }
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code, I: Integer;
begin
  Result := '';
  Exec(ExpandConstant('{sys}\sc.exe'), 'stop Printty', '', SW_HIDE, ewWaitUntilTerminated, Code);
  if Code = 0 then
    for I := 1 to 20 do
    begin
      Sleep(500);
      Exec(ExpandConstant('{cmd}'), '/c sc query Printty | find "STOPPED"', '', SW_HIDE, ewWaitUntilTerminated, Code);
      if Code = 0 then Break;
    end;
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM printty.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Sleep(500);
end;
