-- 欠账口径收口：只有借还（loan）才有「未结清」这回事。
-- 历史上礼物/花销/其它可以随手勾着 settled=0，会被算进首页的「我借出（未还）」。
-- 统计侧已按 kind='loan' 过滤，这里把存量数据也理顺，免得界面出现「未结清的礼物」
-- 却又没有跟进意义。settled_at 不伪造 —— 这些支出本来就没有结清时刻。

UPDATE transactions SET settled=1
WHERE kind <> 'loan' AND settled=0;
