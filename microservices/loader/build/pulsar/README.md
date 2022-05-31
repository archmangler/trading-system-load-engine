# Management service pod build materials

NOTE: ensure the user credentials which will be used for testing the API exist under ./users/ of this directory:

- directory "./users": contains synthetic user credentials in json formatted flat text files (1 file per user credential in the following format:

```
(base) welcome@Traianos-MacBook-Pro pulsar % cat users/test_eqonex_pt_xxx_indi_xxx.json

{
  "email" : "test_eqonex_pt_xxx_indi_xxx@harakirimail.com",
  "password" : "scrambled",
  "secret" : null,
  "userId" : 102529,
  "registrationCode" : null,
"tradeAccount":{
        "tradingAccountName": "null",
        "tradingAccountId": "102529",
        "tradingAccountUuid": "null",
        "tradingRequestSecret": " sdsdsdsdsds",
        "tradingRequestToken": "l8ldAFQE0wG01dX3",
        "submitterRequestSecret": " sdsdsdsdsds",
        "submitterRequestToken": "l8ldAFQE0wG01dX3",
        "submitterId": "102529"
}}%   

```


