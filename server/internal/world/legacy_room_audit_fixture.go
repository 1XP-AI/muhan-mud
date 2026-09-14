package world

// legacyRoomExceptionFixture is the checked-in audit baseline for every
// structurally readable room that the strict body decoder rejects. The hash
// and consumed offset make accidental source edits or silent tail trimming
// visible; the fixture is evidence, not a runtime conversion allow-list.
type legacyRoomExceptionFixture struct {
	path     string
	size     int
	consumed int
	sha256   string
	issues   string
}

var legacyRoomExceptionFixtures = []legacyRoomExceptionFixture{
	{path: "r00/r00100", size: 810, consumed: 810, sha256: "fc7c34ba0e987f60fc3c14299596de2f0bab1f9d64b031e02d6b154b0a47e6c7", issues: "invalid-euc-kr@588"},
	{path: "r00/r00173", size: 2295, consumed: 751, sha256: "7e41f7512bb435c1b880d38cbea982724a1a8c5e61b7e88b5d0201c7911d0259", issues: "trailing-data@751"},
	{path: "r00/r00265", size: 785, consumed: 785, sha256: "525b31d9cf165c9766d2c29b7cbc73abf874989ca8d6a378a2f375384a0724b0", issues: "invalid-euc-kr@632"},
	{path: "r00/r00269", size: 845, consumed: 845, sha256: "7188821c6e454ae5465de7cd8c3dd80655e57f57274d80299a6e919bcf2dbdaf", issues: "invalid-euc-kr@632"},
	{path: "r00/r00277", size: 3652, consumed: 3652, sha256: "9ed37b818338dd58af1d1a5437110d004fbcc0672c2a418fc1c78b7da302e5c7", issues: "invalid-euc-kr@3408"},
	{path: "r00/r00278", size: 1403, consumed: 1047, sha256: "ae13eb708570c524dd6ae112f1b0f1167cca9003745cf5de93d363192ff717d3", issues: "trailing-data@1047"},
	{path: "r00/r00332", size: 819, consumed: 819, sha256: "3e1f03d0d9a02a22137a758d3df1dde43e9d5b2bd277bd28300ee94c0b0b253e", issues: "invalid-euc-kr@632"},
	{path: "r00/r00376", size: 906, consumed: 906, sha256: "b8403bea13338c5b3236a74f7f9c76a9a2540efe8c86e8f709a7b97e766dfda3", issues: "invalid-euc-kr@588"},
	{path: "r00/r00390", size: 2330, consumed: 786, sha256: "e1d10a4e50eb5e5b784ab29ec22a5b1889ddcd38759aa4021cc366883f88d332", issues: "trailing-data@786"},
	{path: "r00/r00400", size: 955, consumed: 955, sha256: "e106b951d3c56f6898c0bb5e4749df2ecb99d22885aa3ee06db0f64fb38a63d6", issues: "invalid-euc-kr@588"},
	{path: "r00/r00412", size: 2502, consumed: 2502, sha256: "8b32721c7fa3a9c8f97a34c34af893c91f1d268eef3beac9a7d8ea42056b6b5f", issues: "invalid-euc-kr@2220"},
	{path: "r00/r00422", size: 770, consumed: 770, sha256: "6d5ed5df767453bd45ed6c4aed902e3b3d721529a1bb2011d5ba21e8d38d9de1", issues: "invalid-euc-kr@588"},
	{path: "r00/r00443", size: 3397, consumed: 1021, sha256: "9993d0a474b9594d4ff7b06f91641eb6a1639794044f079863ec87fa9d04bfc2", issues: "trailing-data@1021"},
	{path: "r00/r00547", size: 833, consumed: 833, sha256: "2d0cd316a9f597320f87a64cf96d7ff0f7a06715d2efb6818f74c675dd1f4b32", issues: "invalid-euc-kr@588"},
	{path: "r00/r00593", size: 2367, consumed: 2367, sha256: "7919d2c0c4d6d9596241a0226812b6a869104e80bc2e2e8a573750590480f549", issues: "invalid-euc-kr@2132"},
	{path: "r00/r00665", size: 1024, consumed: 1024, sha256: "4ad253e98ac24b1efafe47e523c8ddb5603b748953d9aa07521a796145549131", issues: "invalid-euc-kr@588"},
	{path: "r00/r00708", size: 717, consumed: 717, sha256: "fcc5f84ee3e2b3aebb4ba58c9fb85c9aa0615d3c8b340f3cf541335564134195", issues: "invalid-euc-kr@544"},
	{path: "r00/r00748", size: 852, consumed: 852, sha256: "176b7959d4ed0c12f51b72666054f348baeed9960b046eb57d3b56048d04aa4c", issues: "invalid-euc-kr@588"},
	{path: "r00/r00759", size: 912, consumed: 912, sha256: "3565aaf7906d3e206c21aaa9cdc492b9198efc70d4e913ff404348b0be5aad52", issues: "invalid-euc-kr@588"},
	{path: "r01/r00100", size: 810, consumed: 810, sha256: "039afd9f939560eccc4f5d7d17d826693e8ff1a72c285d7bb8f9cb0f7e95868e", issues: "invalid-euc-kr@588"},
	{path: "r01/r00265", size: 785, consumed: 785, sha256: "3805e3143c0e67cc84624440dfe09ecd432da6171c10124d1b76d8c9f81bf480", issues: "invalid-euc-kr@632"},
	{path: "r01/r00269", size: 845, consumed: 845, sha256: "98a8655a28628d28a676bebcdcc28f5a8dd800a51c2c03b7b1a076089e22c558", issues: "invalid-euc-kr@632"},
	{path: "r01/r00277", size: 920, consumed: 920, sha256: "77e55ba267b8dd4f1f43707cff6331e69cdfe5bc4f2fd380e96a39790980ed50", issues: "invalid-euc-kr@676"},
	{path: "r01/r00332", size: 819, consumed: 819, sha256: "ece99ee1407493a55425674375a2f4c946ac65c2bde52af5e43ecf92dea51871", issues: "invalid-euc-kr@632"},
	{path: "r01/r00376", size: 906, consumed: 906, sha256: "55e64a34049e5a18ae795ba437a00c09e3fe027ca67c93bfde966402c2488e3a", issues: "invalid-euc-kr@588"},
	{path: "r01/r00400", size: 955, consumed: 955, sha256: "919103b3cdc49543653003ed7aa9c514434518c2153eae55daf7b2fbbf25fdf4", issues: "invalid-euc-kr@588"},
	{path: "r01/r00412", size: 2502, consumed: 2502, sha256: "f695dca46fb60e14869b7c77a50ace5d25918f91835d3fb9702fb57d437f5000", issues: "invalid-euc-kr@2220"},
	{path: "r01/r00422", size: 770, consumed: 770, sha256: "6b38b277947c6590b51ed7d041c06ee4c064714a4cb861335dd7198ee6a3e9d5", issues: "invalid-euc-kr@588"},
	{path: "r01/r00547", size: 877, consumed: 877, sha256: "3711064c1a952470642d83764dcd02979194ead58f61d55154014638e0213593", issues: "invalid-euc-kr@632"},
	{path: "r01/r00593", size: 823, consumed: 823, sha256: "534a7028ae4af351d3f4c72ead8c054798aec85e2ed90d038211ebe9fb27e913", issues: "invalid-euc-kr@588"},
	{path: "r01/r00665", size: 1024, consumed: 1024, sha256: "9aed5599f6f70f4f8b4d5db213d438dbb4a8cd9d1a4ca89e4dc16e2c0e32e543", issues: "invalid-euc-kr@588"},
	{path: "r01/r00708", size: 717, consumed: 717, sha256: "469defc40d726dd80b562e57a27ece6b84bcc4e3df9c3ba84ebdf0f96225cb03", issues: "invalid-euc-kr@544"},
	{path: "r01/r00748", size: 852, consumed: 852, sha256: "6bb6a139887433d35376dd35458fa8a25960bc7e6311908215cd78c24b7ab9ce", issues: "invalid-euc-kr@588"},
	{path: "r01/r00759", size: 912, consumed: 912, sha256: "4e9e5972c1faaac3f600bdc4985909530720a31783ffec5556d56ffed735f456", issues: "invalid-euc-kr@588"},
	{path: "r03/r03053", size: 2525, consumed: 981, sha256: "f806f0393bcc481acedde8e36e590b4f45a7b14189bc2cd14a059583f3e2059a", issues: "trailing-data@981"},
	{path: "r03/r03073", size: 2080, consumed: 892, sha256: "68bcc4a0ee6635ca77745623aefa56ba6dbd667d2ce258ee8a3040c2606f28ea", issues: "trailing-data@892"},
	{path: "r03/r03200", size: 23934, consumed: 23934, sha256: "ece4666d3a09ae4a1bd4ea7dcd5201af2e2e8e7f8197abd8ccd588aff9b32a47", issues: "invalid-euc-kr@11616"},
	{path: "r03/r03295", size: 10160, consumed: 10160, sha256: "1c753b85cc4102cb64aeacbad0804e7ad213cef88fb85de2c66cda8b1a666599", issues: "invalid-euc-kr@8012,invalid-euc-kr@9436"},
	{path: "r03/r03374", size: 29164, consumed: 29164, sha256: "a8df74636e8b16650e88e1b27d9eb871c59a865b650cb74100738fdf3f6c5344", issues: "invalid-euc-kr@2804,invalid-euc-kr@4584,invalid-euc-kr@5296,invalid-euc-kr@5652,invalid-euc-kr@6008,invalid-euc-kr@18824"},
	{path: "r03/r03377", size: 16212, consumed: 16212, sha256: "41be892884eee283040a07ae7fb0f9a498106cab85852b77af7ad637d72bcbcf", issues: "invalid-euc-kr@1960"},
	{path: "r03/r03380", size: 15688, consumed: 15688, sha256: "d23c59ef06b51fdd0bc70019177369e89d7b200c9bbba1fb6538e1e05ccd0944", issues: "invalid-euc-kr@7564"},
	{path: "r03/r03387", size: 20840, consumed: 20840, sha256: "555b1e00364c0190728060e930f68a39185396bacc40f32200629492de194213", issues: "invalid-euc-kr@892,invalid-euc-kr@1248,invalid-euc-kr@1604,invalid-euc-kr@8368"},
	{path: "r03/r03388", size: 25824, consumed: 25824, sha256: "f8012d6ad4462d9f102bb6190874097775f58a95950c61ec74834b837f8e077b", issues: "invalid-euc-kr@1248,invalid-euc-kr@10504,missing-text-terminator@21760"},
	{path: "r03/r03391", size: 7312, consumed: 7312, sha256: "65d855de945c95e907928f0f8164b7da3ab2d95a312ee3d8f9a33f114b48a92f", issues: "invalid-euc-kr@1960,invalid-euc-kr@2316,invalid-euc-kr@2672,invalid-euc-kr@3028,invalid-euc-kr@3384"},
	{path: "r03/r03398", size: 16565, consumed: 16565, sha256: "f5de51eb8c55ff13d2bd84dfb262e8a4cee8acb316e314b74ad2bf7083391ef9", issues: "invalid-euc-kr@892,invalid-euc-kr@1248,invalid-euc-kr@1604,invalid-euc-kr@1960,invalid-euc-kr@15132"},
	{path: "r03/r03412", size: 8981, consumed: 8981, sha256: "0ac45fa27f2b6188208f2ecd50531ad392f26bcc554992304023e040f38cbfa1", issues: "invalid-euc-kr@892,invalid-euc-kr@1604"},
	{path: "r03/r03420", size: 26570, consumed: 26570, sha256: "3e9427d348969909afcf7bfcef6902b63638c38fce0ac317dc9750c33b70e7ce", issues: "invalid-euc-kr@8012"},
	{path: "r03/r03428", size: 34241, consumed: 34241, sha256: "9091471bcbbd5a9a44f0145093142a4f59435f4305f6b44cadd8c822f6d464fe", issues: "invalid-euc-kr@1648,invalid-euc-kr@18736"},
	{path: "r03/r03429", size: 12952, consumed: 12952, sha256: "b9602d027ca07e64ecab54da4896bb5f6a6164896eecaa53811fe098f9424d24", issues: "invalid-euc-kr@892,invalid-euc-kr@3384,invalid-euc-kr@4808,invalid-euc-kr@5164,invalid-euc-kr@5520,invalid-euc-kr@5876"},
	{path: "r03/r03430", size: 12224, consumed: 12224, sha256: "9d5cd5f9ccda34a49bd4955cbf94815f335a757ff46cea49f94356bb2028349d", issues: "invalid-euc-kr@892,invalid-euc-kr@1248,invalid-euc-kr@1604"},
	{path: "r03/r03435", size: 12569, consumed: 12569, sha256: "863f31a06192cfd7352202b05066dad3f620a7bb0b92c238dd90e373e0124de6", issues: "invalid-euc-kr@892,invalid-euc-kr@3740,invalid-euc-kr@4096"},
	{path: "r03/r03438", size: 23909, consumed: 23909, sha256: "91aef3197eaf50efc3dfea4d188623b05a585b08501cc34c52297eab95167fe5", issues: "missing-text-terminator@15352"},
	{path: "r03/r03449", size: 48179, consumed: 48179, sha256: "2c06578b5e5ab21fa5735ae0472de4a861ffc1d61f6f5f2156189f7c1009b8c9", issues: "missing-text-terminator@43120"},
	{path: "r03/r03460", size: 19197, consumed: 19197, sha256: "41c418c2b83ae9dc64549fce392799716fbe4f6a6ad74be7e310863d1acb431f", issues: "invalid-euc-kr@2004,invalid-euc-kr@10904"},
	{path: "r03/r03462", size: 33374, consumed: 33374, sha256: "93e742945367bd5ec247675df70e3fa5fd2ef3f5b166da3a0d24af629fa24733", issues: "missing-text-terminator@4672,missing-text-terminator@5028,missing-text-terminator@5384,missing-text-terminator@5740,missing-text-terminator@6096,missing-text-terminator@6452"},
	{path: "r03/r03464", size: 35589, consumed: 35589, sha256: "473219b858a8276ec7a1c45eb56c7feee973e866c3f7bf349b038f153231ba44", issues: "missing-text-terminator@21804"},
	{path: "r03/r03466", size: 27180, consumed: 27180, sha256: "9465e5aacc28cbbbfa7c0fb881764953e6f45cf63a3cc19273eb71af6ffdd655", issues: "invalid-euc-kr@26532"},
	{path: "r03/r03468", size: 32565, consumed: 32565, sha256: "d79f96e16ddac0a405d2387072d5cb5c50164ffec88f3a94a1720f5b28ea2390", issues: "missing-text-terminator@18956,missing-text-terminator@19312"},
	{path: "r03/r03482", size: 35080, consumed: 35080, sha256: "b4eb2c797ac234c06d4f1c6c5418fa1e78c6625c639262fc68e3dc91cab20767", issues: "missing-text-terminator@27456"},
	{path: "r03/r03502", size: 2151, consumed: 963, sha256: "e50a5c4f73fd9b738353a42fa65484df640c83ff9efeb3ce54e6bdefaaa354fd", issues: "trailing-data@963"},
	{path: "r05/r00547", size: 877, consumed: 877, sha256: "c28a97dedb78b99de6fe72ee05fc44694218ee432affe3a2c81570c744242364", issues: "invalid-euc-kr@632"},
	{path: "r05/r00593", size: 2367, consumed: 2367, sha256: "beddff71366f2db0b0122e6ce5beef7f7c1f46f1c18fdd45c0498e560d90ac5f", issues: "invalid-euc-kr@2132"},
	{path: "r05/r05029", size: 1095, consumed: 1095, sha256: "9cff2b07cabe58ae5ce8585f03f0261da5bb6de6f4afa28526560a0045b94eb5", issues: "invalid-euc-kr@900"},
}

// legacyTrailingDataAdmissionFixtures is the explicit conversion allow-list
// for leftover tails. Admission keeps every original byte and decodes only
// through Consumed. It is not a trim list and does not admit invalid EUC-KR
// or missing text terminators.
var legacyTrailingDataAdmissionFixtures = []legacyRoomExceptionFixture{
	{path: "r00/r00173", size: 2295, consumed: 751, sha256: "7e41f7512bb435c1b880d38cbea982724a1a8c5e61b7e88b5d0201c7911d0259", issues: "trailing-data@751"},
	{path: "r00/r00278", size: 1403, consumed: 1047, sha256: "ae13eb708570c524dd6ae112f1b0f1167cca9003745cf5de93d363192ff717d3", issues: "trailing-data@1047"},
	{path: "r00/r00390", size: 2330, consumed: 786, sha256: "e1d10a4e50eb5e5b784ab29ec22a5b1889ddcd38759aa4021cc366883f88d332", issues: "trailing-data@786"},
	{path: "r00/r00443", size: 3397, consumed: 1021, sha256: "9993d0a474b9594d4ff7b06f91641eb6a1639794044f079863ec87fa9d04bfc2", issues: "trailing-data@1021"},
	{path: "r03/r03053", size: 2525, consumed: 981, sha256: "f806f0393bcc481acedde8e36e590b4f45a7b14189bc2cd14a059583f3e2059a", issues: "trailing-data@981"},
	{path: "r03/r03073", size: 2080, consumed: 892, sha256: "68bcc4a0ee6635ca77745623aefa56ba6dbd667d2ce258ee8a3040c2606f28ea", issues: "trailing-data@892"},
	{path: "r03/r03502", size: 2151, consumed: 963, sha256: "e50a5c4f73fd9b738353a42fa65484df640c83ff9efeb3ce54e6bdefaaa354fd", issues: "trailing-data@963"},
}

// legacyMissingTextTerminatorAdmissionFixtures is the explicit conversion
// allow-list for fixed-width text that fills the C field without a NUL.
// Admission keeps every original byte and reads the unterminated field to
// its C field boundary. It is not a NUL-synthesis or trim list and does
// not admit invalid EUC-KR or trailing data. r03/r03388 stays out because
// it also has invalid-euc-kr issues.
var legacyMissingTextTerminatorAdmissionFixtures = []legacyRoomExceptionFixture{
	{path: "r03/r03438", size: 23909, consumed: 23909, sha256: "91aef3197eaf50efc3dfea4d188623b05a585b08501cc34c52297eab95167fe5", issues: "missing-text-terminator@15352"},
	{path: "r03/r03449", size: 48179, consumed: 48179, sha256: "2c06578b5e5ab21fa5735ae0472de4a861ffc1d61f6f5f2156189f7c1009b8c9", issues: "missing-text-terminator@43120"},
	{path: "r03/r03462", size: 33374, consumed: 33374, sha256: "93e742945367bd5ec247675df70e3fa5fd2ef3f5b166da3a0d24af629fa24733", issues: "missing-text-terminator@4672,missing-text-terminator@5028,missing-text-terminator@5384,missing-text-terminator@5740,missing-text-terminator@6096,missing-text-terminator@6452"},
	{path: "r03/r03464", size: 35589, consumed: 35589, sha256: "473219b858a8276ec7a1c45eb56c7feee973e866c3f7bf349b038f153231ba44", issues: "missing-text-terminator@21804"},
	{path: "r03/r03468", size: 32565, consumed: 32565, sha256: "d79f96e16ddac0a405d2387072d5cb5c50164ffec88f3a94a1720f5b28ea2390", issues: "missing-text-terminator@18956,missing-text-terminator@19312"},
	{path: "r03/r03482", size: 35080, consumed: 35080, sha256: "b4eb2c797ac234c06d4f1c6c5418fa1e78c6625c639262fc68e3dc91cab20767", issues: "missing-text-terminator@27456"},
}

// legacyInvalidEUCKRAdmissionFixtures is the explicit conversion allow-list
// for NUL-terminated text that is not strict EUC-KR. Admission keeps every
// original byte and decodes with U+FFFD substitution only; it must not invent
// Hangul from non-EUC-KR sequences. Mixed r03/r03388 is in this list because
// invalid-euc-kr is the first issue class. This is not a CP949 mapping or
// source-repair list and does not admit trailing data.
var legacyInvalidEUCKRAdmissionFixtures = []legacyRoomExceptionFixture{
	{path: "r00/r00100", size: 810, consumed: 810, sha256: "fc7c34ba0e987f60fc3c14299596de2f0bab1f9d64b031e02d6b154b0a47e6c7", issues: "invalid-euc-kr@588"},
	{path: "r00/r00265", size: 785, consumed: 785, sha256: "525b31d9cf165c9766d2c29b7cbc73abf874989ca8d6a378a2f375384a0724b0", issues: "invalid-euc-kr@632"},
	{path: "r00/r00269", size: 845, consumed: 845, sha256: "7188821c6e454ae5465de7cd8c3dd80655e57f57274d80299a6e919bcf2dbdaf", issues: "invalid-euc-kr@632"},
	{path: "r00/r00277", size: 3652, consumed: 3652, sha256: "9ed37b818338dd58af1d1a5437110d004fbcc0672c2a418fc1c78b7da302e5c7", issues: "invalid-euc-kr@3408"},
	{path: "r00/r00332", size: 819, consumed: 819, sha256: "3e1f03d0d9a02a22137a758d3df1dde43e9d5b2bd277bd28300ee94c0b0b253e", issues: "invalid-euc-kr@632"},
	{path: "r00/r00376", size: 906, consumed: 906, sha256: "b8403bea13338c5b3236a74f7f9c76a9a2540efe8c86e8f709a7b97e766dfda3", issues: "invalid-euc-kr@588"},
	{path: "r00/r00400", size: 955, consumed: 955, sha256: "e106b951d3c56f6898c0bb5e4749df2ecb99d22885aa3ee06db0f64fb38a63d6", issues: "invalid-euc-kr@588"},
	{path: "r00/r00412", size: 2502, consumed: 2502, sha256: "8b32721c7fa3a9c8f97a34c34af893c91f1d268eef3beac9a7d8ea42056b6b5f", issues: "invalid-euc-kr@2220"},
	{path: "r00/r00422", size: 770, consumed: 770, sha256: "6d5ed5df767453bd45ed6c4aed902e3b3d721529a1bb2011d5ba21e8d38d9de1", issues: "invalid-euc-kr@588"},
	{path: "r00/r00547", size: 833, consumed: 833, sha256: "2d0cd316a9f597320f87a64cf96d7ff0f7a06715d2efb6818f74c675dd1f4b32", issues: "invalid-euc-kr@588"},
	{path: "r00/r00593", size: 2367, consumed: 2367, sha256: "7919d2c0c4d6d9596241a0226812b6a869104e80bc2e2e8a573750590480f549", issues: "invalid-euc-kr@2132"},
	{path: "r00/r00665", size: 1024, consumed: 1024, sha256: "4ad253e98ac24b1efafe47e523c8ddb5603b748953d9aa07521a796145549131", issues: "invalid-euc-kr@588"},
	{path: "r00/r00708", size: 717, consumed: 717, sha256: "fcc5f84ee3e2b3aebb4ba58c9fb85c9aa0615d3c8b340f3cf541335564134195", issues: "invalid-euc-kr@544"},
	{path: "r00/r00748", size: 852, consumed: 852, sha256: "176b7959d4ed0c12f51b72666054f348baeed9960b046eb57d3b56048d04aa4c", issues: "invalid-euc-kr@588"},
	{path: "r00/r00759", size: 912, consumed: 912, sha256: "3565aaf7906d3e206c21aaa9cdc492b9198efc70d4e913ff404348b0be5aad52", issues: "invalid-euc-kr@588"},
	{path: "r01/r00100", size: 810, consumed: 810, sha256: "039afd9f939560eccc4f5d7d17d826693e8ff1a72c285d7bb8f9cb0f7e95868e", issues: "invalid-euc-kr@588"},
	{path: "r01/r00265", size: 785, consumed: 785, sha256: "3805e3143c0e67cc84624440dfe09ecd432da6171c10124d1b76d8c9f81bf480", issues: "invalid-euc-kr@632"},
	{path: "r01/r00269", size: 845, consumed: 845, sha256: "98a8655a28628d28a676bebcdcc28f5a8dd800a51c2c03b7b1a076089e22c558", issues: "invalid-euc-kr@632"},
	{path: "r01/r00277", size: 920, consumed: 920, sha256: "77e55ba267b8dd4f1f43707cff6331e69cdfe5bc4f2fd380e96a39790980ed50", issues: "invalid-euc-kr@676"},
	{path: "r01/r00332", size: 819, consumed: 819, sha256: "ece99ee1407493a55425674375a2f4c946ac65c2bde52af5e43ecf92dea51871", issues: "invalid-euc-kr@632"},
	{path: "r01/r00376", size: 906, consumed: 906, sha256: "55e64a34049e5a18ae795ba437a00c09e3fe027ca67c93bfde966402c2488e3a", issues: "invalid-euc-kr@588"},
	{path: "r01/r00400", size: 955, consumed: 955, sha256: "919103b3cdc49543653003ed7aa9c514434518c2153eae55daf7b2fbbf25fdf4", issues: "invalid-euc-kr@588"},
	{path: "r01/r00412", size: 2502, consumed: 2502, sha256: "f695dca46fb60e14869b7c77a50ace5d25918f91835d3fb9702fb57d437f5000", issues: "invalid-euc-kr@2220"},
	{path: "r01/r00422", size: 770, consumed: 770, sha256: "6b38b277947c6590b51ed7d041c06ee4c064714a4cb861335dd7198ee6a3e9d5", issues: "invalid-euc-kr@588"},
	{path: "r01/r00547", size: 877, consumed: 877, sha256: "3711064c1a952470642d83764dcd02979194ead58f61d55154014638e0213593", issues: "invalid-euc-kr@632"},
	{path: "r01/r00593", size: 823, consumed: 823, sha256: "534a7028ae4af351d3f4c72ead8c054798aec85e2ed90d038211ebe9fb27e913", issues: "invalid-euc-kr@588"},
	{path: "r01/r00665", size: 1024, consumed: 1024, sha256: "9aed5599f6f70f4f8b4d5db213d438dbb4a8cd9d1a4ca89e4dc16e2c0e32e543", issues: "invalid-euc-kr@588"},
	{path: "r01/r00708", size: 717, consumed: 717, sha256: "469defc40d726dd80b562e57a27ece6b84bcc4e3df9c3ba84ebdf0f96225cb03", issues: "invalid-euc-kr@544"},
	{path: "r01/r00748", size: 852, consumed: 852, sha256: "6bb6a139887433d35376dd35458fa8a25960bc7e6311908215cd78c24b7ab9ce", issues: "invalid-euc-kr@588"},
	{path: "r01/r00759", size: 912, consumed: 912, sha256: "4e9e5972c1faaac3f600bdc4985909530720a31783ffec5556d56ffed735f456", issues: "invalid-euc-kr@588"},
	{path: "r03/r03200", size: 23934, consumed: 23934, sha256: "ece4666d3a09ae4a1bd4ea7dcd5201af2e2e8e7f8197abd8ccd588aff9b32a47", issues: "invalid-euc-kr@11616"},
	{path: "r03/r03295", size: 10160, consumed: 10160, sha256: "1c753b85cc4102cb64aeacbad0804e7ad213cef88fb85de2c66cda8b1a666599", issues: "invalid-euc-kr@8012,invalid-euc-kr@9436"},
	{path: "r03/r03374", size: 29164, consumed: 29164, sha256: "a8df74636e8b16650e88e1b27d9eb871c59a865b650cb74100738fdf3f6c5344", issues: "invalid-euc-kr@2804,invalid-euc-kr@4584,invalid-euc-kr@5296,invalid-euc-kr@5652,invalid-euc-kr@6008,invalid-euc-kr@18824"},
	{path: "r03/r03377", size: 16212, consumed: 16212, sha256: "41be892884eee283040a07ae7fb0f9a498106cab85852b77af7ad637d72bcbcf", issues: "invalid-euc-kr@1960"},
	{path: "r03/r03380", size: 15688, consumed: 15688, sha256: "d23c59ef06b51fdd0bc70019177369e89d7b200c9bbba1fb6538e1e05ccd0944", issues: "invalid-euc-kr@7564"},
	{path: "r03/r03387", size: 20840, consumed: 20840, sha256: "555b1e00364c0190728060e930f68a39185396bacc40f32200629492de194213", issues: "invalid-euc-kr@892,invalid-euc-kr@1248,invalid-euc-kr@1604,invalid-euc-kr@8368"},
	{path: "r03/r03388", size: 25824, consumed: 25824, sha256: "f8012d6ad4462d9f102bb6190874097775f58a95950c61ec74834b837f8e077b", issues: "invalid-euc-kr@1248,invalid-euc-kr@10504,missing-text-terminator@21760"},
	{path: "r03/r03391", size: 7312, consumed: 7312, sha256: "65d855de945c95e907928f0f8164b7da3ab2d95a312ee3d8f9a33f114b48a92f", issues: "invalid-euc-kr@1960,invalid-euc-kr@2316,invalid-euc-kr@2672,invalid-euc-kr@3028,invalid-euc-kr@3384"},
	{path: "r03/r03398", size: 16565, consumed: 16565, sha256: "f5de51eb8c55ff13d2bd84dfb262e8a4cee8acb316e314b74ad2bf7083391ef9", issues: "invalid-euc-kr@892,invalid-euc-kr@1248,invalid-euc-kr@1604,invalid-euc-kr@1960,invalid-euc-kr@15132"},
	{path: "r03/r03412", size: 8981, consumed: 8981, sha256: "0ac45fa27f2b6188208f2ecd50531ad392f26bcc554992304023e040f38cbfa1", issues: "invalid-euc-kr@892,invalid-euc-kr@1604"},
	{path: "r03/r03420", size: 26570, consumed: 26570, sha256: "3e9427d348969909afcf7bfcef6902b63638c38fce0ac317dc9750c33b70e7ce", issues: "invalid-euc-kr@8012"},
	{path: "r03/r03428", size: 34241, consumed: 34241, sha256: "9091471bcbbd5a9a44f0145093142a4f59435f4305f6b44cadd8c822f6d464fe", issues: "invalid-euc-kr@1648,invalid-euc-kr@18736"},
	{path: "r03/r03429", size: 12952, consumed: 12952, sha256: "b9602d027ca07e64ecab54da4896bb5f6a6164896eecaa53811fe098f9424d24", issues: "invalid-euc-kr@892,invalid-euc-kr@3384,invalid-euc-kr@4808,invalid-euc-kr@5164,invalid-euc-kr@5520,invalid-euc-kr@5876"},
	{path: "r03/r03430", size: 12224, consumed: 12224, sha256: "9d5cd5f9ccda34a49bd4955cbf94815f335a757ff46cea49f94356bb2028349d", issues: "invalid-euc-kr@892,invalid-euc-kr@1248,invalid-euc-kr@1604"},
	{path: "r03/r03435", size: 12569, consumed: 12569, sha256: "863f31a06192cfd7352202b05066dad3f620a7bb0b92c238dd90e373e0124de6", issues: "invalid-euc-kr@892,invalid-euc-kr@3740,invalid-euc-kr@4096"},
	{path: "r03/r03460", size: 19197, consumed: 19197, sha256: "41c418c2b83ae9dc64549fce392799716fbe4f6a6ad74be7e310863d1acb431f", issues: "invalid-euc-kr@2004,invalid-euc-kr@10904"},
	{path: "r03/r03466", size: 27180, consumed: 27180, sha256: "9465e5aacc28cbbbfa7c0fb881764953e6f45cf63a3cc19273eb71af6ffdd655", issues: "invalid-euc-kr@26532"},
	{path: "r05/r00547", size: 877, consumed: 877, sha256: "c28a97dedb78b99de6fe72ee05fc44694218ee432affe3a2c81570c744242364", issues: "invalid-euc-kr@632"},
	{path: "r05/r00593", size: 2367, consumed: 2367, sha256: "beddff71366f2db0b0122e6ce5beef7f7c1f46f1c18fdd45c0498e560d90ac5f", issues: "invalid-euc-kr@2132"},
	{path: "r05/r05029", size: 1095, consumed: 1095, sha256: "9cff2b07cabe58ae5ce8585f03f0261da5bb6de6f4afa28526560a0045b94eb5", issues: "invalid-euc-kr@900"},
}
