# finalperf05 全部采样（失败不删）

10预热+100请求；p50/p95最近秩。旧版同期仍用原固定archive二进制。门禁列同时列原旧版/同期旧版；正式深页预算相对同轮首批。宽松查询独立报告。

## candidate-finalperf05

|查询|范围|页|p50 ms|p95 ms|/原旧版|/同期旧版|/本轮首批|门禁(原/同期)|
|---|---|---|---:|---:|---:|---:|---:|---|
|海|series|first|101.389|229.227|9.5106|3.9724|—|FAIL/FAIL|
|海|series|middle|98.865|186.568|—|—|0.8139|PASS|
|海|series|near_end|84.047|179.450|—|—|0.7828|PASS|
|海|book|first|1168.501|1777.489|1.5690|2.8141|—|PASS/FAIL|
|海|book|middle|1422.874|2450.237|—|—|1.3785|PASS|
|海|book|near_end|1440.297|2411.304|—|—|1.3566|PASS|
|海贼|series|first|58.460|135.342|1.9263|3.1784|—|PASS/FAIL|
|海贼|series|middle|32.425|169.250|—|—|1.2505|PASS|
|海贼|series|near_end|67.489|311.464|—|—|2.3013|FAIL|
|海贼|book|first|1087.284|2490.218|2.1017|4.1679|—|FAIL/FAIL|
|海贼|book|middle|1245.572|2385.712|—|—|0.9580|PASS|
|海贼|book|near_end|1061.870|1713.399|—|—|0.6881|PASS|
|海贼王|series|first|43.880|838.470|4.0410|8.4521|—|FAIL/FAIL|
|海贼王|series|middle|92.123|289.932|—|—|0.3458|PASS|
|海贼王|series|near_end|70.967|217.208|—|—|0.2591|PASS|
|海贼王|book|first|461.658|1526.771|0.3083|0.2746|—|PASS/PASS|
|海贼王|book|middle|449.615|510.840|—|—|0.3346|PASS|
|海贼王|book|near_end|445.911|516.593|—|—|0.3384|PASS|
|星|series|first|2.154|4.808|0.4093|0.3874|—|PASS/PASS|
|星|book|first|3.563|8.456|0.8499|0.2273|—|PASS/PASS|
|星|book|near_end|3.732|12.892|—|—|1.5246|PASS|
|孤星|series|first|2.700|6.571|0.9511|0.1800|—|PASS/PASS|
|孤星|book|first|3.825|10.415|0.5945|0.2331|—|PASS/PASS|
|孤星|book|near_end|4.119|12.860|—|—|1.2347|PASS|
|孤星Atlas|series|first|2.778|10.408|0.7904|0.3707|—|PASS/PASS|
|孤星Atlas|book|first|4.912|14.769|0.3278|0.1878|—|PASS/PASS|
|孤星Atlas|book|near_end|4.593|16.708|—|—|1.1313|PASS|
|孤—星 Ａｔｌａｓ|series|first|2.482|6.618|—|—|—|独立|
|孤—星 Ａｔｌａｓ|book|first|4.643|11.891|—|—|—|独立|
|孤—星 Ａｔｌａｓ|book|near_end|4.420|15.290|—|—|1.2859|PASS|

## candidate-finalperf05-repeat

|查询|范围|页|p50 ms|p95 ms|/原旧版|/同期旧版|/本轮首批|门禁(原/同期)|
|---|---|---|---:|---:|---:|---:|---:|---|
|海|series|first|36.947|62.735|2.6029|1.0872|—|FAIL/PASS|
|海|series|middle|40.239|50.768|—|—|0.8092|PASS|
|海|series|near_end|31.150|40.561|—|—|0.6465|PASS|
|海|book|first|946.503|1055.410|0.9316|1.6709|—|PASS/PASS|
|海|book|middle|912.483|1073.998|—|—|1.0176|PASS|
|海|book|near_end|975.898|1143.843|—|—|1.0838|PASS|
|海贼|series|first|54.807|67.005|0.9537|1.5735|—|PASS/PASS|
|海贼|series|middle|60.213|74.420|—|—|1.1107|PASS|
|海贼|series|near_end|44.046|62.438|—|—|0.9318|PASS|
|海贼|book|first|798.124|973.585|0.8217|1.6295|—|PASS/PASS|
|海贼|book|middle|985.150|1529.422|—|—|1.5709|PASS|
|海贼|book|near_end|733.613|1080.213|—|—|1.1095|PASS|
|海贼王|series|first|37.862|46.454|0.2239|0.4683|—|PASS/PASS|
|海贼王|series|middle|39.510|50.760|—|—|1.0927|PASS|
|海贼王|series|near_end|27.163|34.120|—|—|0.7345|PASS|
|海贼王|book|first|560.208|623.673|0.1259|0.1122|—|PASS/PASS|
|海贼王|book|middle|626.728|723.063|—|—|1.1594|PASS|
|海贼王|book|near_end|582.067|651.274|—|—|1.0443|PASS|
|星|series|first|2.174|4.619|0.3932|0.3722|—|PASS/PASS|
|星|book|first|3.664|6.114|0.6145|0.1643|—|PASS/PASS|
|星|book|near_end|3.586|6.944|—|—|1.1358|PASS|
|孤星|series|first|2.331|4.742|0.6863|0.1299|—|PASS/PASS|
|孤星|book|first|3.327|5.938|0.3389|0.1329|—|PASS/PASS|
|孤星|book|near_end|3.624|7.547|—|—|1.2710|PASS|
|孤星Atlas|series|first|3.738|7.786|0.5913|0.2773|—|PASS/PASS|
|孤星Atlas|book|first|4.427|7.220|0.1602|0.0918|—|PASS/PASS|
|孤星Atlas|book|near_end|5.053|9.607|—|—|1.3305|PASS|
|孤—星 Ａｔｌａｓ|series|first|2.296|4.102|—|—|—|独立|
|孤—星 Ａｔｌａｓ|book|first|4.508|7.111|—|—|—|独立|
|孤—星 Ａｔｌａｓ|book|near_end|4.693|7.457|—|—|1.0487|PASS|

## 同期固定旧版首批

|查询|范围|p50 ms|p95 ms|
|---|---|---:|---:|
|海|series|40.540|57.705|
|海|book|566.770|631.633|
|海贼|series|32.799|42.582|
|海贼|book|530.031|597.477|
|海贼王|series|76.346|99.202|
|海贼王|book|2148.519|5560.011|
|星|series|1.510|12.411|
|星|book|6.745|37.208|
|孤星|series|2.002|36.517|
|孤星|book|7.027|44.679|
|孤星Atlas|series|2.512|28.079|
|孤星Atlas|book|16.780|78.659|
