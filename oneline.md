Paste into an elevated Command Prompt on the new PC:                                                                                      
                                                                                                                                           
 ```cmd                                                                                                                                    
   net use \\10.3.122.102\software-scripts /user:dedibeat * && powershell -NoProfile -ExecutionPolicy Bypass -File                         
 \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl http://10.3.122.102:8080                                                  
 ```                                                                                                                                       
                                                                                                                                           
 It prompts Type the password for \\10.3.122.102\software-scripts: — type RoomDeploy-2026.                                                 
                                                                                                                                           
 Why the *: it makes net use prompt for the password instead of embedding it in the command line, so the password doesn't land in the      
 session history. If you'd rather skip the prompt:                                                                                         
                                                                                                                                           
 ```cmd                                                                                                                                    
   net use \\10.3.122.102\software-scripts /user:dedibeat RoomDeploy-2026 && powershell -NoProfile -ExecutionPolicy Bypass -File           
 \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl http://10.3.122.102:8080                                                  
 ```                                                                                                                                       
                                                                                                                                           
 Change room-302 for a different room. Everything else stays fixed.                                                                        
                                                                                                                                           
 Parts: net use … && authenticates (the && stops everything if the password is wrong) → -ExecutionPolicy Bypass is required because the    
 script lives on a UNC path → install.ps1 runs elevated because the prompt already is.                                                     
                                                                                                                                           
 ────────────────────────────────────────────────────────────────────────────────                                                          
                                                                                                                                           
 Two variants you may want                                                                                                                 
                                                                                                                                           
 On a re-imaged PC that might hold a stale credential — clears it first, ignores failure:                                                  
                                                                                                                                           
 ```cmd                                                                                                                                    
   cmdkey /delete:10.3.122.102 >nul 2>&1 & net use \\10.3.122.102\software-scripts /delete /y >nul 2>&1 & net use                          
 \\10.3.122.102\software-scripts /user:dedibeat * && powershell -NoProfile -ExecutionPolicy Bypass -File                                   
 \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl http://10.3.122.102:8080                                                  
 ```                                                                                                                                       
                                                                                                                                           
 Self-elevating, so you can run it from a normal prompt (raises the UAC box itself). I have not tested this on Windows — quoting through   
 Start-Process is the fragile part, so treat it as a convenience, not the reliable path:                                                   
                                                                                                                                           
 ```cmd                                                                                                                                    
   powershell -NoProfile -Command "Start-Process cmd -Verb RunAs -ArgumentList '/c net use \\10.3.122.102\software-scripts /user:dedibeat  
 * && powershell -NoProfile -ExecutionPolicy Bypass -File \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl                  
 http://10.3.122.102:8080 & pause'"                                                                                                        
 ```                                                                                                                                       
                                                                                                                                           
 ────────────────────────────────────────────────────────────────────────────────                                                          
                                                                                                                                           
 Honest status                                                                                                                             
                                                                                                                                           
 The SMB auth path and the HTTP download path are both verified from Linux. The net use + -File invocation itself I cannot test — no       
 Windows machine here. Try it on one PC before trusting it across rooms.                                                                   
                                                                                                                                           
 Also still true: Room 302 is complete: false with 4 pending requirement groups, so this installs 8 packages, not a finished room.