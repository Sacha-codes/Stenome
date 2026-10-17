
#define MyAppName "Stenome"
#define MyAppVersion "1.0.0"
#define MyAppExeName "Stenome.exe"

[Setup]
AppId=B17F5D73-9A52-4D36-9E51-7DCD4F2B816A
AppName={#MyAppName}
AppVersion={#MyAppVersion}
DefaultDirName={autopf}\Stenome
DefaultGroupName=Stenome
UninstallDisplayIcon={app}\Stenome.exe
SetupIconFile=StenoIco.ico
OutputDir=Output
OutputBaseFilename=StenomeSetup
Compression=lzma
SolidCompression=yes
WizardStyle=modern

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "Stenome.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Stenome"; Filename: "{app}\Stenome.exe"
Name: "{autodesktop}\Stenome"; Filename: "{app}\Stenome.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\Stenome.exe"; Description: "Launch Stenome"; Flags: postinstall nowait skipifsilent
