# 组清单（真实数据快照）

这份清单不是凭印象列的，是从站点上真实发布的资源里统计出来的，用来给
`config.yaml` 的 `rules.primary` 挑组名。改配置前先在这里查一下组名的准确写法。

## 数据来源

| 项目 | 值 |
| --- | --- |
| AnimeGarden 资源样本 | 最近 10000 条（`GET https://api.animes.garden/resources?pageSize=1000&page=1..10`） |
| AnimeGarden 注册字幕组 | 727 个（`GET https://api.animes.garden/teams`） |
| 样本里实际有发布的组 | 63 个 |
| Nyaa 标题样本 | 最近 1800 条（`https://nyaa.si/?c=1_0|1_1|1_2|1_3&p=1..8`） |

**注意**：AnimeGarden 注册了 727 个组，但样本里只有 63 个组真的在发资源。
配置里优先写下面出现过的组名，注册表里那些长期不更新的组写了也匹配不到东西。

## 怎么在配置里用

`rules.primary` 的 `match` 是正则，匹配「字幕组名 + 最近的资源标题」，
所以语言类型（简日 / 繁日 / 内封）和组名可以写在同一个列表里，靠 `priority` 排序。

```yaml
rules:
  primary:
    - {name: 简日内嵌, match: '(?i)简日|简体|CHS', priority: 10, scope: main}
    - {name: 桜都字幕组, match: '(?i)sakurato|桜都',   priority: 40, scope: main}
```

写组名时注意：

- 中文组名直接写汉字即可；同一个组在 dmhy / 蜜柑 / 萌番组上的名字可能不同，用 `|` 把几种写法都串上
- 日文组名常有简体/繁体/日文汉字三种写法（例如 `雪飄工作室` / `雪飘工作室`），繁简都写
- 带特殊符号的组名要转义：`NEO·QSW` 写成 `NEO·QSW`，`❀拨雪寻春❀` 直接写 `拨雪寻春` 更稳

## 字幕组（标题里有语言标记）

| 组名 | 样本条数 | 语言 / 封装画像 |
| --- | ---: | --- |
| Nix-Raws | 922 | 简繁 921、内封 921、WEB 922 |
| ANi | 747 | 简繁 3、繁体 744、内封 3、WEB 747、HEVC 3 |
| LoliHouse | 738 | 简日 3、繁日 129、简繁 666、简繁日 129、内封 649 |
| 绿茶字幕组 | 290 | 简日 103、繁日 187、简繁 85、简繁日 85、内嵌 205 |
| 桜都字幕组 | 269 | 简繁 90、简体 89、繁体 90、内嵌 179、内封 90 |
| GMTeam | 231 | 简体 225、4K 130、HEVC 127、10bit 53 |
| 喵萌奶茶屋 | 225 | 简日 92、繁日 107、简繁 13、简繁日 13、简体 13 |
| 天月動漫發佈組 | 168 | 简体 30、繁体 129、多语 6、WEB 135、HEVC 3 |
| 猎户发布组 | 152 | 简日 76、繁日 76、内嵌 152 |
| 悠哈璃羽字幕社 | 148 | 简日 9、繁日 24、简繁 19、简繁日 14、简体 66 |
| 爱恋字幕社 | 124 | 简体 50、繁体 50、WEB 4 |
| 三明治摆烂组 | 100 | 简日 43、繁日 57、简繁 14、简繁日 14、内嵌 86 |
| 北宇治字幕组 | 96 | 简日 29、繁日 56、简繁 31、简繁日 28、内嵌 65 |
| 沸班亚马制作组 | 94 | 简繁 94、内封 94、WEB 94、4K 65、HEVC 88 |
| TSDM字幕组 | 89 | 简日 66、繁日 23、简繁 12、简繁日 12、简体 25 |
| Zzz睡不醒 | 73 | 繁日 18、简繁 47、简繁日 18、内封 48、BD 31 |
| Prejudice-Studio | 71 | 简日 5、繁日 13、简繁 13、简繁日 13、简体 52 |
| 雪飄工作室 | 67 | 简日 13、繁日 50、简繁 26、简繁日 25、繁体 1 |
| 拨雪寻春 | 62 | 简日 21、繁日 41、简繁 21、简繁日 21、内嵌 41 |
| 极影字幕社 | 62 | 简体 16、繁体 16 |
| 银色子弹字幕组 | 60 | 简日 19、繁日 41、简繁 22、简繁日 22、BD 15 |
| 百冬練習組 | 58 | 简体 29、繁体 29、WEB 16 |
| 亿次研同好会 | 57 | 简日 15、繁日 30、简繁 15、简繁日 15、内嵌 30 |
| Nesuto个人发布 | 50 | 简日 11、繁日 37、简繁 39、简繁日 37、内封 49 |
| 豌豆字幕组 | 48 | 简体 24、繁体 24、BD 2、WEB 2 |
| SweetSub | 48 | 简日 19、繁日 29、简繁 10、简繁日 10、内封 10 |
| 黑白字幕组 | 46 | 简体 23、繁体 23、WEB 46、HEVC 22 |
| 夜莺家族 | 45 | 简日 12、内嵌 1、生肉 10、BD 1、合集 1 |
| MingYSub | 36 | 简日 12、繁日 24、简繁 12、简繁日 12、内嵌 24 |
| 樱桃花字幕组 | 33 | 简日 32、内嵌 32 |
| 咪路fans制作组 | 33 | 简日 15、简体 5、4K 1 |
| 云光字幕组 | 28 | 简体 28、合集 5 |
| 驯兽师联盟 | 27 | 简日 13、繁日 14 |
| 丸子家族 | 23 | 简日 1、简体 11、繁体 11、内嵌 1、BD 1 |
| 得宗字幕组 | 22 | 简体 11、繁体 11 |
| 动漫国字幕组 | 18 | 简体 9、繁体 9、BD 4 |
| 追放字幕组 | 14 | 简体 7、繁体 7 |
| SFSub | 12 | 简日 3、繁日 3、简体 1、繁体 6、内嵌 6 |
| 冷番补完字幕组 | 11 | 简繁 7、内封 11 |
| ToonsHub | 7 | 简体 6、繁体 5、WEB 2、合集 3 |
| 六四位元字幕組 | 6 | 简体 1、繁体 4、BD 1、10bit 1 |
| DBD制作组 | 5 | 简繁 2、内封 3、BD 5、HEVC 5、10bit 5 |
| 艾莉希婭 | 4 | 简繁 2、繁体 1、内封 1、BD 1、WEB 3 |
| 哆啦字幕组 | 3 | 简日 2、繁日 2 |
| 星塔汉化组 | 2 | 简日 1、简体 1、内嵌 2、WEB 1 |
| 诸神字幕组 | 1 | 简日 1、简体 1、BD 1、HEVC 1 |

## 压制组（走 BD / HEVC / 10bit 路线）

| 组名 | 样本条数 | 语言 / 封装画像 |
| --- | ---: | --- |
| VCB-Studio | 20 | BD 20、HEVC 19、10bit 20 |
| H-Enc | 12 | BD 12、HEVC 12 |
| 未央阁联盟 | 7 | BD 3、4K 4、HEVC 7、10bit 4 |
| Clarita 压制组 | 1 | 繁体 1、内封 1、WEB 1、HEVC 1、10bit 1 |
| 新Sub | 1 | 简日 1、HEVC 1、10bit 1 |

## 搬运 / 聚合发布（产量极高，但认不出语言标记）

这些组的产量能排到全站前列，但标题里几乎没有「简日 / 繁日 / 内封」这类标记，
说明它们发的是生肉搬运或聚合转发，不是自己做字幕。
默认规则下它们会靠「发布条数」这个兜底排序挤进副 RSS —— 真实跑出来的
计划里，`Kirara Fantasia` 出现在十条副 RSS 里的八条。不想要就写进 `rules.deny`。

| 组名 | 样本条数 | 语言 / 封装画像 |
| --- | ---: | --- |
| Kirara Fantasia | 2833 | 4K 10、HEVC 411 |
| 魔星字幕团 | 402 | 简繁 46、WEB 402、合集 12 |
| 萝莉社活动室 | 215 | 简体 1、繁体 1 |

## 样本太少、认不出性质

样本里只出现个位数次，语言标记也不明显。想用的话先点进站点确认它在发什么。

| 组名 | 样本条数 | 语言 / 封装画像 |
| --- | ---: | --- |
| 天使动漫论坛 | 5 | （标题里没有可识别的语言/封装标记） |
| NEO·QSW | 4 | WEB 4 |
| FSD粉羽社 | 3 | （标题里没有可识别的语言/封装标记） |
| DBFC字幕组 | 2 | （标题里没有可识别的语言/封装标记） |
| 漫游字幕组 | 1 | 内嵌 1、4K 1、合集 1 |
| 百冬练习组 | 1 | 繁体 1、WEB 1 |
| 芝士动物朋友 | 1 | 简繁 1、内封 1、WEB 1、HEVC 1 |
| PorterRAWS | 1 | WEB 1 |
| 零始字幕組 | 1 | BD 1 |

## Nyaa 放流组

Nyaa 没有「字幕组」维度，RSS 一律是关键字搜索型。AniSync 会搜一次，
从标题里提取 `[组名]`，再给每个组生成一个限定搜索词的 RSS。
下表按「有单集特征的标题条数」排序，只保留真正在发周更番剧的组。

| 组名 | 单集条数 | 总条数 | 备注 | 样例标题 |
| --- | ---: | ---: | --- | --- |
| Doomdos | 174 | 174 | 国漫 | [Doomdos] - Shiguang Dailiren - S03E12 [1080p BILIBILI COM WEB-DL] [Link Click… |
| Erai-raws | 89 | 92 |  | [Erai-raws] FX Senshi Kurumi-chan - 04 [480p CR WEB-DL AVC AAC][MultiSub][901F… |
| SubsPlease | 66 | 90 |  | [SubsPlease] FX Senshi Kurumi-chan - 04 (1080p) [F89CC7E7].mkv |
| ToonsHub | 60 | 63 |  | [ToonsHub] FX Fighter Kurumi-chan S01E04 1080p CR WEB-DL DUAL AAC2.0 H.264 (FX… |
| ANi | 55 | 55 |  | [ANi] FX Senshi KURUMICHAN /  FX 戰士久留美 - 04 [1080P][Baha][WEB-DL][AAC AVC][CHT… |
| LoliHouse | 34 | 37 |  | [LoliHouse] GIRLS BAND CRY 1st Album [棘アリ] [BDRip 1080p HEVC-10bit FLAC] |
| AnoZu | 29 | 31 |  | [AnoZu] FX Fighter Kurumi-chan S01E04 1080p CR WEB-DL Dual-Audio DDP 2.0 H.264… |
| GM-Team | 27 | 27 | 国漫 | [GM-Team][国漫][灵境行者][Spirit Realm Walker][2026][09][HEVC][GB][4K] |
| Yameii | 22 | 22 |  | [Yameii] The World&#39;s Strongest Witch - S01E01 [English Dub] [CR WEB-DL 108… |
| ASW | 21 | 22 |  | [ASW] Kikansha no Mahou wa Tokubetsu desu - 15 [1080p HEVC x265 10Bit][AAC] |
| 桜都字幕组 | 18 | 18 |  | [桜都字幕组] FX战士久留美 / FX Senshi Kurumi-chan [03][1080P][简繁内封] |
| DKB | 16 | 17 |  | [DKB] Cyberpunk Edgerunners - (Season 02) [1080p][HEVC x265 10bit][Dual-Audio]… |
| Judas | 14 | 16 |  | [Judas] Kikansha no Mahou wa Tokubetsu desu (A Returner&#39;s Magic Should Be … |
| Shridhuu | 12 | 14 | 国漫 | [Shridhuu][1080p] Spare Me, Great Lord! / 大王饶命 / Da Wang Rao Ming - S03E05 |
| 绿茶字幕组 | 12 | 13 |  | [绿茶字幕组] 僵尸搜寻中 / Zombie Sagashitemasu [02][WebRip][1080p][简繁日内封] |
| Gecko | 8 | 9 | 国漫 | [Gecko] The Guardian of Daxia - S01E10 (大夏守墓人; Daxia Shou Mu Ren) [WETV.WEB-DL… |
| WF | 7 | 7 |  | [WF] BLEACH: Sennen Kessen-hen - 49 [WEB-DL CR 1080p x264 AAC][PT-BR] (Bleach:… |
| Anime Time | 6 | 8 |  | [Anime Time] Reincarnated As A Sword - S02E01 [1080p][HEVC 10bit x265][AAC][Du… |
| Feibanyama | 6 | 6 |  | [Feibanyama] As a Reincarnated Aristocrat Ill Use My Appraisal Skill to Rise i… |
| 綠茶字幕組 | 6 | 6 |  | [綠茶字幕組] 描繪直至生命盡頭 / 畫完這個就去死 / Kore Kaite Shine [08][WebRip][1080p][繁日內嵌] |
| Ironclad | 5 | 6 |  | [Ironclad] Tensei Shitara Ken Deshita - S02E04 [WEB.1080p.AV1] \| Reincarnated… |
| Diogo4D | 5 | 6 |  | [Diogo4D] [BD][1080p] Turma de Elite 3.ª Temporada - 02 (Youkoso Jitsuryoku Sh… |
| FLsnow | 5 | 5 |  | [FLsnow][おねがいアイプリ/Onegai_Aipri][1080P][29][CHS/CHT&amp;JPN] |
| 喵萌奶茶屋&amp;LoliHouse | 4 | 4 |  | [喵萌奶茶屋&amp;LoliHouse] 被放逐的作弊赋予魔法师享受随心所欲的第二人生。~不只是武器，我还能赋予万物「强化点数」，且能照我的意思随时解除，… |
| SFSub | 4 | 4 |  | [SFSub] FX战士久留美 / FX Senshi Kurumi-chan - 03 [WebRip][1080P][AVC][AAC][简日内嵌][M… |
| ❀拨雪寻春❀ | 3 | 4 |  | [❀拨雪寻春❀] 恶女不才，请多包涵 ~雏宫蝶鼠换身传~ / ふつつかな悪女ではございますが / Futsutsuka na Akujo de wa Goz… |
| TSPlease | 3 | 3 |  | [TSPlease] The Laid-Off Cheat-Granting Mage Enjoys a Second Lease on Life S01E… |
| 三明治摆烂组&amp;LoliHouse | 3 | 3 |  | [三明治摆烂组&amp;LoliHouse] 乱世千金倪亚·利斯顿转生为娇弱千金的弑神武人华丽无双录 / Kyouran Reijou Nia Liston… |
| FênixFansub | 3 | 3 |  | [FênixFansub] Kyouran Reijou Nia Liston - 03 [PT-BR] |
| 银色子弹字幕组 | 3 | 3 |  | [银色子弹字幕组][名侦探柯南][第1217集 背后的推手][WEBRIP][简繁日多语MKV][PGS][1080P] |
| Billion Meta Lab | 3 | 3 |  | [Billion Meta Lab] 魔法姐妹露露莉莉 Mahou no Shimai Rurutto Riryi [14][1080P][HEVC-10b… |
| Trix | 2 | 5 |  | [Trix] The Laid-Off Cheat-Granting Mage Enjoys a Second Lease on Life S01E03 [… |
| Kekkan | 2 | 3 |  | [Kekkan] Kyouran Reijou Nia Liston - S01E02 (WebRip 1080p AV1 Multi-Subs) / 凶乱… |
| SubsPlus+ | 2 | 2 |  | [SubsPlus+] Reincarnated as a Sword - S02E04 (ADN WEB-DL 1080p AVC AAC) \| Ten… |
| Buzz-Subs | 2 | 2 |  | [Buzz-Subs] Black Clover (2017) S02E03 (1080p Web-Encode AV1) [Eng-Subs].mkv |
| 绿茶字幕组&amp;LoliHouse | 2 | 2 |  | [绿茶字幕组&amp;LoliHouse] 描绘直至生命尽头 / 画完这个就去死 / 畫完這個再去死 / これ描いて死ね / Kore Kaite Shin… |
| 北宇治字幕组 | 2 | 2 |  | [北宇治字幕组] FX戰士久留美 / FX Senshi Kurumi-chan [02][WebRip][HEVC_AAC][繁日內嵌] |
| Naruto-Kun.Hu | 2 | 2 |  | [Naruto-Kun.Hu] Dragon Ball Super - Beerus - 02 [1080p].mkv |
| KiyoshiiSubs | 2 | 2 |  | [KiyoshiiSubs] Bleach Sennen Kessen-hen - 49 [1080p][H.265 - 10Bit].mkv |
| NKWT | 2 | 2 |  | [NKWT] Shuiro no Kamen - 02 [1080p].mkv |

「国漫」标记的是华语原声/国漫放流（例如 `Doomdos` 的《时光代理人》、`GM-Team` 的
《灵境行者》）。追日本番剧的话把它们写进 `rules.deny`，否则它们会占掉副 RSS 的位置。

## 刷新这份清单

```bash
# 1. 注册的全部字幕组（带 dmhy / 萌番组 / 蜜柑 三站 ID，可用于反查别名）
curl 'https://api.animes.garden/teams' -o teams.json

# 2. 最近资源（fansub 字段就是组名，title 里有语言与封装标记）
curl 'https://api.animes.garden/resources?pageSize=1000&page=1' -o res1.json

# 3. Nyaa 各分类的最近标题
curl 'https://nyaa.si/?c=1_2&f=0&p=1' -o nyaa1.html
```
