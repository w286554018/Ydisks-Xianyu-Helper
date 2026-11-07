import { useCallback, useEffect, useRef, useState } from 'react';
import { createKeyword, deleteKeyword, fetchKeywordTrend, fetchKeywords, fetchMomentum, fetchOverview, fetchSellerBoard, triggerScan, updateKeyword } from './api';
import type { AggregateSummary, KeywordListResult, MomentumSummary, ScanResult, SellerBoardData, TrendData, WatchedKeyword } from './types';

/** MarketIntelState 是市场情报区块的全部状态与动作。 */
export interface MarketIntelState {
  // momentum 是热词涨幅榜。
  momentum: MomentumSummary | null;
  // momentumLoading 表示涨幅加载中。
  momentumLoading: boolean;
  // momentumError 是涨幅加载错误。
  momentumError: string;
  // keywords 是关注词表。
  keywords: KeywordListResult | null;
  // keywordsLoading 表示词表加载中。
  keywordsLoading: boolean;
  // keywordsError 是词表错误。
  keywordsError: string;
  // newKeyword 是新词输入。
  newKeyword: string;
  // setNewKeyword 更新新词输入。
  setNewKeyword: (value: string) => void;
  // submitting 表示词表写操作执行中。
  submitting: boolean;
  // addKeyword 提交新增。
  addKeyword: () => Promise<void>;
  // toggleKeyword 切换启用状态。
  toggleKeyword: (item: WatchedKeyword) => Promise<void>;
  // removeKeyword 删除关注词。
  removeKeyword: (item: WatchedKeyword) => Promise<void>;
  // addFromMomentum 从热词涨幅榜一键关注。
  addFromMomentum: (keyword: string) => Promise<void>;
  // trend 是当前查看的趋势。
  trend: TrendData | null;
  // trendLoading 表示趋势加载中。
  trendLoading: boolean;
  // trendError 是趋势错误。
  trendError: string;
  // selectKeyword 查看单词趋势。
  selectKeyword: (item: WatchedKeyword) => Promise<void>;
  // closeTrend 关闭趋势视图。
  closeTrend: () => void;
  // aggregates 是各词最新聚合。
  aggregates: AggregateSummary[];
  // aggregatesLoading 表示聚合加载中。
  aggregatesLoading: boolean;
  // board 是卖家榜。
  board: SellerBoardData | null;
  // boardLoading 表示卖家榜加载中。
  boardLoading: boolean;
  // boardError 是卖家榜错误。
  boardError: string;
  // scanning 表示扫描执行中。
  scanning: boolean;
  // scanMessage 是最近一次扫描的结果说明。
  scanMessage: string;
  // scan 执行手动扫描并刷新数据。
  scan: () => Promise<void>;
  // refreshOverview 刷新概览（涨幅 + 聚合 + 卖家榜）。
  refreshOverview: () => Promise<void>;
  // loadMomentumPublic 供面板刷新按钮使用。
  loadMomentumPublic: () => Promise<void>;
}

/** useMarketIntel 管理市场情报区块的数据加载与动作。 */
export const useMarketIntel = (): MarketIntelState => {
  // momentum 是热词涨幅榜。
  const [momentum, setMomentum] = useState<MomentumSummary | null>(null);
  // momentumLoading 表示涨幅加载中。
  const [momentumLoading, setMomentumLoading] = useState(false);
  // momentumError 是涨幅加载错误。
  const [momentumError, setMomentumError] = useState('');
  // keywords 是关注词表。
  const [keywords, setKeywords] = useState<KeywordListResult | null>(null);
  // keywordsLoading 表示词表加载中。
  const [keywordsLoading, setKeywordsLoading] = useState(false);
  // keywordsError 是词表错误。
  const [keywordsError, setKeywordsError] = useState('');
  // newKeyword 是新词输入。
  const [newKeyword, setNewKeyword] = useState('');
  // submitting 表示词表写操作执行中。
  const [submitting, setSubmitting] = useState(false);
  // trend 是当前查看的趋势。
  const [trend, setTrend] = useState<TrendData | null>(null);
  // trendLoading 表示趋势加载中。
  const [trendLoading, setTrendLoading] = useState(false);
  // trendError 是趋势错误。
  const [trendError, setTrendError] = useState('');
  // aggregates 是各词最新聚合。
  const [aggregates, setAggregates] = useState<AggregateSummary[]>([]);
  // aggregatesLoading 表示聚合加载中。
  const [aggregatesLoading, setAggregatesLoading] = useState(false);
  // board 是卖家榜。
  const [board, setBoard] = useState<SellerBoardData | null>(null);
  // boardLoading 表示卖家榜加载中。
  const [boardLoading, setBoardLoading] = useState(false);
  // boardError 是卖家榜错误。
  const [boardError, setBoardError] = useState('');
  // scanning 表示扫描执行中。
  const [scanning, setScanning] = useState(false);
  // scanMessage 是最近一次扫描的结果说明。
  const [scanMessage, setScanMessage] = useState('');
  // controllers 保存全部在途请求的取消器；组件卸载时统一中止。
  const controllers = useRef<Set<AbortController>>(new Set());
  // unmounted 标记组件是否已卸载；卸载后所有晚到响应一律丢弃。
  const unmounted = useRef(false);

  // 组件卸载时中止全部未完成请求并标记卸载。
  useEffect(
    () => (
      /* 卸载清理回调中止所有在途网络请求并标记卸载。 */
      () => {
        unmounted.current = true;
        controllers.current.forEach(/* inFlightController 中止单个在途请求。 */ controller => controller.abort());
        controllers.current.clear();
      }
    ),
    [],
  );

  // beginRequest 创建动作独立的取消器；并行请求互不取消、互不判废。
  const beginRequest = useCallback(/* beginRequest 的回调创建独立取消器。 */ () => {
    // next 是本次动作独享的取消器。
    const next = new AbortController();
    controllers.current.add(next);
    // done 在请求结束时把取消器移出在途集合。
    const done = () => controllers.current.delete(next);
    // settled 判断本次请求是否已被中止或组件已卸载。
    const settled = () => unmounted.current || next.signal.aborted;
    return { signal: next.signal, done, settled };
  }, []);

  // loadKeywords 刷新词表。
  const loadKeywords = useCallback(/* loadKeywordsAction 刷新词表。 */ async () => {
    // request 是本次读取的独立取消器与代次。
    const request = beginRequest();
    setKeywordsLoading(true);
    setKeywordsError('');
    try {
      // result 是词表读取结果。
      const result = await fetchKeywords({ signal: request.signal });
      if (request.settled()) return;
      setKeywords(result);
    } catch (error /* 词表读取错误 */) {
      if (request.settled()) return;
      setKeywordsError(error instanceof Error ? error.message : '读取关注词失败');
    } finally {
      request.done();
      if (!request.settled()) setKeywordsLoading(false);
    }
  }, [beginRequest]);

  // loadMomentum 刷新热词涨幅。
  const loadMomentum = useCallback(/* loadMomentumAction 刷新热词涨幅。 */ async () => {
    // request 是本次读取的独立取消器与代次。
    const request = beginRequest();
    setMomentumLoading(true);
    setMomentumError('');
    try {
      // result 是涨幅读取结果。
      const result = await fetchMomentum({ signal: request.signal });
      if (request.settled()) return;
      setMomentum(result);
    } catch (error /* 涨幅读取错误 */) {
      if (request.settled()) return;
      setMomentumError(error instanceof Error ? error.message : '读取热词涨幅失败');
    } finally {
      request.done();
      if (!request.settled()) setMomentumLoading(false);
    }
  }, [beginRequest]);

  // refreshOverview 刷新概览（涨幅 + 聚合 + 卖家榜）。
  const refreshOverview = useCallback(/* refreshOverviewAction 刷新概览。 */ async () => {
    // request 是本次读取的独立取消器与代次。
    const request = beginRequest();
    setAggregatesLoading(true);
    setBoardLoading(true);
    setBoardError('');
    try {
      // overview 是概览读取结果。
      const overview = await fetchOverview({ signal: request.signal });
      if (request.settled()) return;
      setMomentum(overview.momentum);
      setAggregates(overview.keywords);
    } catch (error /* 概览读取错误 */) {
      if (request.settled()) return;
      console.error('读取市场概览失败:', error);
    } finally {
      if (!request.settled()) setAggregatesLoading(false);
    }
    try {
      // boardData 是卖家榜读取结果。
      const boardData = await fetchSellerBoard(30, { signal: request.signal });
      if (request.settled()) return;
      setBoard(boardData);
    } catch (error /* 卖家榜读取错误 */) {
      if (request.settled()) return;
      setBoardError(error instanceof Error ? error.message : '读取卖家榜失败');
    } finally {
      request.done();
      if (!request.settled()) setBoardLoading(false);
    }
  }, [beginRequest]);

  // 首屏并行加载词表、涨幅与概览。
  useEffect(
    /* initialLoadEffect 首屏并行加载全部数据。 */
    () => {
      void loadKeywords();
      void loadMomentum();
      void refreshOverview();
    },
    [loadKeywords, loadMomentum, refreshOverview],
  );

  // addKeyword 提交新增并刷新词表。
  const addKeyword = useCallback(/* addKeywordAction 提交新增关注词。 */ async () => {
    // trimmed 是去空白后的新词。
    const trimmed = newKeyword.trim();
    if (!trimmed || submitting) return;
    setSubmitting(true);
    setKeywordsError('');
    try {
      await createKeyword(trimmed, true);
      setNewKeyword('');
      await loadKeywords();
    } catch (error /* 新增错误 */) {
      setKeywordsError(error instanceof Error ? error.message : '添加关注词失败');
    } finally {
      setSubmitting(false);
    }
  }, [loadKeywords, newKeyword, submitting]);

  // addFromMomentum 从热词涨幅榜一键关注。
  const addFromMomentum = useCallback(/* addFromMomentumAction 把热词加入词表。 */ async (keyword: string) => {
    if (submitting) return;
    setSubmitting(true);
    setKeywordsError('');
    try {
      await createKeyword(keyword, true);
      await loadKeywords();
    } catch (error /* 关注错误 */) {
      setKeywordsError(error instanceof Error ? error.message : '添加关注词失败');
    } finally {
      setSubmitting(false);
    }
  }, [loadKeywords, submitting]);

  // toggleKeyword 切换启用状态。
  const toggleKeyword = useCallback(/* toggleKeywordAction 切换词启用状态。 */ async (item: WatchedKeyword) => {
    setSubmitting(true);
    setKeywordsError('');
    try {
      await updateKeyword(item.id, item.keyword, !item.enabled);
      await loadKeywords();
    } catch (error /* 切换错误 */) {
      setKeywordsError(error instanceof Error ? error.message : '更新关注词失败');
    } finally {
      setSubmitting(false);
    }
  }, [loadKeywords, submitting]);

  // removeKeyword 删除关注词。
  const removeKeyword = useCallback(/* removeKeywordAction 删除关注词。 */ async (item: WatchedKeyword) => {
    if (!confirm(`确认删除关注词「${item.keyword}」吗？历史扫描数据将保留。`)) return;
    setSubmitting(true);
    setKeywordsError('');
    try {
      await deleteKeyword(item.id);
      if (trend?.keywordId === item.id) setTrend(null);
      await loadKeywords();
    } catch (error /* 删除错误 */) {
      setKeywordsError(error instanceof Error ? error.message : '删除关注词失败');
    } finally {
      setSubmitting(false);
    }
  }, [loadKeywords, submitting, trend?.keywordId]);

  // selectKeyword 查看单词趋势。
  const selectKeyword = useCallback(/* selectKeywordAction 加载单词趋势。 */ async (item: WatchedKeyword) => {
    // request 是本次读取的独立取消器与代次。
    const request = beginRequest();
    setTrendLoading(true);
    setTrendError('');
    setTrend(null);
    try {
      // result 是趋势读取结果。
      const result = await fetchKeywordTrend(item.id, 30, { signal: request.signal });
      if (request.settled()) return;
      setTrend(result);
    } catch (error /* 趋势读取错误 */) {
      if (request.settled()) return;
      setTrendError(error instanceof Error ? error.message : '读取趋势失败');
    } finally {
      request.done();
      if (!request.settled()) setTrendLoading(false);
    }
  }, [beginRequest]);

  // closeTrend 关闭趋势视图。
  const closeTrend = useCallback(/* closeTrendAction 关闭趋势视图。 */ () => {
    setTrend(null);
    setTrendError('');
  }, []);

  // scan 执行手动扫描并刷新全部数据。
  const scan = useCallback(/* scanAction 手动触发扫描。 */ async () => {
    if (scanning) return;
    setScanning(true);
    setScanMessage('');
    try {
      // result 是扫描结果摘要。
      const result: ScanResult = await triggerScan();
      // message 是组装的结果说明。
      const message = result.stoppedReason
        ? `扫描中止：${result.stoppedReason}（已扫 ${result.scanned} 词）`
        : `扫描完成：本次 ${result.scanned} 词，跳过 ${result.skipped} 词${result.verdictFailed > 0 ? `，${result.verdictFailed} 词判定失败` : ''}`;
      setScanMessage(message);
      await Promise.all([loadKeywords(), loadMomentum(), refreshOverview()]);
    } catch (error /* 扫描错误 */) {
      setScanMessage(error instanceof Error ? error.message : '执行扫描失败');
    } finally {
      setScanning(false);
    }
  }, [loadKeywords, loadMomentum, refreshOverview, scanning]);

  return {
    momentum, momentumLoading, momentumError,
    keywords, keywordsLoading, keywordsError,
    newKeyword, setNewKeyword, submitting,
    addKeyword, toggleKeyword, removeKeyword, addFromMomentum,
    trend, trendLoading, trendError, selectKeyword, closeTrend,
    aggregates, aggregatesLoading,
    board, boardLoading, boardError,
    scanning, scanMessage, scan, refreshOverview, loadMomentumPublic: loadMomentum,
  };
};
