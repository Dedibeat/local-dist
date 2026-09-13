# Remaining room installation plan

Source: `MTES_program_installation_by_room-2.xlsx`

Rooms 405, 406, and 109 are already complete. The remaining rooms are 208, 209, 301, 302, and 407.

## Added to the provider

The first package batch is ready and assigned to the rooms that require it:

- Node.js LTS 24.21.0
- Git for Windows 2.55.0.5, including Git Bash
- Visual Studio Code 1.137.0
- w64devkit 2.9.1, including GCC

Each installer was downloaded from its official publisher URL and checked against the publisher's SHA-256 value. Room plans remain marked incomplete until their pending requirements are added.

## Room 208

Confirmed requirements:

- Anaconda, Python, and Visual Studio Code
- CircuitVerse, MARIE Simulator, EMU8086, and a C/C++ or Python setup in Visual Studio Code
- Code::Blocks
- Dev-C++
- Java JDK
- Linux and a C compiler
- MySQL, Microsoft SQL Server, and PostgreSQL
- Node.js, TypeScript, Visual Studio Code, Arduino IDE, and Git for Windows
- Raptor

Needs clarification:

- The workbook has no software information for the Probability Systems Analysis course taught by Д.Тэмүүжин.

## Room 209

Confirmed requirements:

- Anaconda
- Android Studio, Flutter SDK, and Visual Studio Code
- CircuitVerse, MARIE Simulator, EMU8086, and a C/C++ or Python setup in Visual Studio Code
- Linux and a C compiler
- GNU Octave
- MySQL, Microsoft SQL Server, and PostgreSQL
- Node.js, MongoDB, Visual Studio Code, and Chrome
- Node.js, TypeScript, Visual Studio Code, Arduino IDE, and Git for Windows
- RStudio
- Raptor
- Visual Studio Code and a GCC toolchain

Needs clarification:

- The Algorithm Analysis and Design 1 row contains only `?`.
- The Software Engineering Fundamentals row says `ашиглахгүй` in the main column but lists Docker, Python, and MySQL in an extra column.

## Room 301

Confirmed requirements:

- Anaconda, Python, and Visual Studio Code
- Android Studio, Flutter SDK, and Visual Studio Code
- w64devkit, Git for Windows, and the Windows SSH client
- Visual Studio Code with a C/C++ toolchain
- CircuitVerse, MARIE Simulator, EMU8086, and a C/C++ or Python setup in Visual Studio Code
- Dev-C++
- A Linux dual-boot environment
- Microsoft Office
- MySQL, Microsoft SQL Server, and PostgreSQL

Needs clarification:

- The Algorithm Analysis and Design 1 row contains only `?`.
- The workbook has no software information for the Probability Systems Analysis course taught by Д.Тэмүүжин.

## Room 302

Confirmed requirements:

- Anaconda, Python, and Visual Studio Code
- Android Studio, Flutter SDK, and Visual Studio Code
- CircuitVerse, MARIE Simulator, EMU8086, and a C/C++ or Python setup in Visual Studio Code
- Dev-C++
- MySQL and Microsoft SQL Server
- PostgreSQL Server
- Raptor

Needs clarification:

- The workbook has no software information for the Probability Systems Analysis course taught by Д.Тэмүүжин.

## Room 407

Confirmed requirements:

- Anaconda
- Dev-C++
- A Linux dual-boot environment
- GNU Octave
- Node.js, TypeScript, Visual Studio Code, Arduino IDE, and Git for Windows
- Visual Studio Code and a GCC toolchain

The workbook does not list any unresolved course requirements for Room 407.

## Decisions needed before creating deployable packages

- Decide whether Python should come only from Anaconda or also be installed separately.
- Decide which GCC environment to use. The workbook mentions both w64devkit and generic GCC.
- Decide whether Linux means dual boot, a virtual machine, or WSL. The current Windows deployment script cannot create a safe dual-boot layout.
- Choose the required editions and versions of MySQL, Microsoft SQL Server, PostgreSQL, Microsoft Office, Android Studio, Flutter, and Java.
- Confirm whether database courses need full database servers on every computer or only client tools.
- Confirm licensing and installer sources for EMU8086, Microsoft SQL Server, Microsoft Office, and any other restricted software.
- Treat CircuitVerse as a browser service unless an offline version is specifically required.
- Treat `Java GDK` in the workbook as `Java JDK`; this appears to be a spelling error.
- Treat `MATLAB Open Source (Octave)` as GNU Octave. MATLAB itself is a separate licensed product.
- Treat GitBash as part of Git for Windows and TypeScript as a package installed after Node.js.

These choices affect installer filenames, silent-install arguments, checksums, detection rules, and disk usage. They should be fixed before adding entries to the live `data/catalog.json`.
