INDEX  STATUS      BYTES      PROBED  PATH                                                                                     ERROR
001    cached      194526842  false   /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/k6EQAOmJrbw.webm  
002    downloaded  17817243   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/8UVNT4wvIGY.webm  
003    downloaded  115043651  true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/HSe-l0HFvx4.mkv   
004    downloaded  25157682   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/UePtoxDhJSw.webm  
005    downloaded  12765192   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/Ardc3nrQMxw.webm  
006    downloaded  322710552  true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/NPcyTyilmYY.webm  
007    downloaded  7370908    true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/GKyvMFkuCog.webm  
008    downloaded  16979640   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/Nv8VfV-18xo.webm  
009    downloaded  24544956   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/CcNo07Xp8aQ.webm  
010    downloaded  13655650   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/desJKYvdq9A.webm  
011    downloaded  7756243    true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/9cxwbJIU2b8.webm  
012    downloaded  9037852    true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/iRyKLQWY9CA.webm  
013    error       0          false                                                                                            stat local source "/Users/pierce/Repos/power-hour-generator/sample_project/Avril Lavigne - Sk8er Boi (Official Video) - YouTube": stat /Users/pierce/Repos/power-hour-generator/sample_project/Avril Lavigne - Sk8er Boi (Official Video) - YouTube: no such file or directory
014    downloaded  21346239   true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/3YxaaGgTQYM.webm  
015    downloaded  514640608  true    /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/6Ejga4kJUts.webm  
Downloaded: 13, Copied: 0, Cached: 1, Probed: 12, Failed: 1

Ok so this is roughly the output from fetch at the end. This is more useful than the huge stream, I'd like to add log levels. At info, default, we just output this format but fill it in iteratively as we fill each row so it starts out something like 
INDEX  STATUS      BYTES      PROBED  PATH                                                                                     ERROR
001    DOWNLOADING      10 (1%)  false   /Users/pierce/Repos/power-hour-generator/sample_project/.powerhour/src/k6EQAOmJrbw.webm  
Then fills in with the report data? Note that bytes shows a % when downloading, and we should update STATUS as a given element is downloaded, processed etc

Vebose logs as it does now, with all output mirrored to the terminal and the report at the end