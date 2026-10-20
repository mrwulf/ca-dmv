# Third-party content

The question bank in `questions/questions.json` and the images in `web/signs/` combine the following sources.

## MIT licensed

- [Steven-Tibbs/road-ready-ca](https://github.com/Steven-Tibbs/road-ready-ca): questions and explanations from `js/questions.js`.
- [jedi116/dmv-knowledge-test-ca](https://github.com/jedi116/dmv-knowledge-test-ca): questions, explanations, and sign SVGs.

Both are used under the MIT License. Each copyright notice and permission notice is available at the linked repository's `LICENSE` file.

## Sign images

Most sign images in `web/signs/` are the official US Manual on Uniform Traffic Control Devices (MUTCD) artwork, public domain as a US government work, taken from Wikimedia Commons. Commons file titles: `MUTCD R1-1` (stop), `R1-2` (yield), `R3-4` (no U-turn), `R4-3` (slower traffic keep right), `R5-1` (do not enter), `D9-2` (hospital), `Mile Marker 4`, `Speed Limit 25 sign`, `Speed Limit 65 sign`, `MUTCD CW20-1` (road work), `Ped Signal - Steady hand` (pedestrian signal), `W10-1` (railroad crossing), `S1-1` (school), `W11-3` (deer), `W4-2R` (lane ends), `W8-5` (slippery when wet), `W6-3` (two-way traffic), `W1-5R` (winding road), and `W1-2R` (curve). Where a file had no `viewBox`, one was added so it scales. California's signs follow the same symbols, but small details can differ from the MUTCD versions.

The traffic signal faces (`signal-*.svg`) and the two-way left-turn lane diagram were drawn for this project (MIT, like the code), because the handbook's own signal pictures are only about 60 pixels wide and blur when enlarged. The curb, lane-marking, lane-control, park, and freeway guide pictures come from the MIT-licensed bank above; captions that gave away the answer were removed.

## Driver Handbook illustrations

The pictures in `web/handbook/` and the three hand-signal pictures (`web/signs/hand-signal-*.png`) are illustrations from the California Driver's Handbook. They are the turn, parking, passing, and similar diagrams shown with a question's explanation after answering, plus a few shown with the question. The handbook states: "This publication is licensed under the Creative Commons Attribution-NonCommercial 4.0 International License." Credit line required by the DMV: **California Driver's Handbook, California Department of Motor Vehicles, licensed under CC BY-NC 4.0.** That license allows copying and adapting with credit for non-commercial use only, and the DMV states that commercial use needs its prior written permission. This app is free and non-commercial; if that ever changes, remove or replace this material first. The credit line is shown on the app's home screen.

## California DMV sample tests

Question and answer-option text for the questions with source `dmv-sample-1` to `dmv-sample-4` comes from the California Department of Motor Vehicles' published
[sample Class C knowledge tests](https://www.dmv.ca.gov/portal/driver-education-and-safety/educational-materials/sample-driver-license-dl-knowledge-tests/),
© State of California. The answers and explanations for those questions were written for this project from the California Driver Handbook, with a link to the supporting handbook section where there is one, not taken from the DMV.

## Disclaimer

This is an unofficial study aid and is not affiliated with or endorsed by the California DMV. Laws change; check the current
[California Driver Handbook](https://www.dmv.ca.gov/portal/handbook/california-driver-handbook/) before your test.
