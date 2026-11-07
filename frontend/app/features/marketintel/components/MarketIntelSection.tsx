import React from 'react';
import { Radar, ScanSearch } from 'lucide-react';
import { KeywordAggregatesTable, KeywordManagerPanel, KeywordTrendPanel, SellerBoardPanel } from '../components/MarketIntelPanels';
import { CompetitorDetailModal } from '../components/CompetitorDetailModal';
import { MomentumPanel } from '../components/MomentumPanel';
import { useMarketIntel, useCompetitorDetail } from '../hooks';
import { createPortal } from 'react-dom';

/** MarketIntelSection 是选品参考页的市场情报区块：关注词、热词涨幅、趋势与竞品卖家。 */
const MarketIntelSection: React.FC = () => {
  // market 是区块全部状态与动作。
  const market = useMarketIntel();
  // competitor 是竞品详情弹窗与搬运动作。
  const competitor = useCompetitorDetail();

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Radar className="h-5 w-5 text-brand" />
          <h2 className="text-base font-extrabold text-gray-900">市场情报</h2>
          <span className="text-xs text-gray-400">每日自动扫描关注词，AI 判定 AI 相关产品需求</span>
        </div>
        <button
          type="button"
          onClick={/* 手动触发扫描动作。 */ () => void market.scan()}
          disabled={market.scanning}
          className="rounded-xl bg-gray-900 px-4 py-2 text-xs font-extrabold text-white hover:bg-black disabled:opacity-50"
        >
          <ScanSearch className={`mr-1 inline h-3.5 w-3.5 ${market.scanning ? 'animate-pulse' : ''}`} />
          {market.scanning ? '扫描中（串行执行，请耐心等待）...' : '立即扫描'}
        </button>
      </div>
      {market.scanMessage && (
        <div className="rounded-xl bg-blue-50 px-4 py-2.5 text-xs font-bold text-blue-800">{market.scanMessage}</div>
      )}
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <KeywordManagerPanel
          keywords={market.keywords}
          loading={market.keywordsLoading}
          error={market.keywordsError}
          newKeyword={market.newKeyword}
          onNewKeywordChange={market.setNewKeyword}
          onAdd={/* 新增动作提交关注词。 */ () => void market.addKeyword()}
          onToggle={/* 切换动作更新启用状态。 */ item => void market.toggleKeyword(item)}
          onDelete={/* 删除动作移除关注词。 */ item => void market.removeKeyword(item)}
          onSelect={/* 选择动作加载趋势。 */ item => void market.selectKeyword(item)}
          submitting={market.submitting}
        />
        <MomentumPanel
          momentum={market.momentum}
          loading={market.momentumLoading}
          error={market.momentumError}
          onRefresh={/* 刷新动作重载涨幅榜。 */ () => void market.loadMomentumPublic()}
          onUseAsKeyword={/* 关注动作把热词加入词表。 */ keyword => void market.addFromMomentum(keyword)}
        />
      </div>
      {(market.trend || market.trendError) && (
        <KeywordTrendPanel
          trend={market.trend}
          loading={market.trendLoading}
          error={market.trendError}
          onClose={market.closeTrend}
        />
      )}
      <KeywordAggregatesTable aggregates={market.aggregates} loading={market.aggregatesLoading} />
      {market.aggregates.length > 0 && (
        <div className="rounded-xl bg-gray-50 px-4 py-3 text-xs text-gray-500">
          每个词每天自动抓取 30 件在售商品。点击下方「查看商品」可打开该词的商品列表，进入商品详情后可 AI 改写文案并一键搬运。
        </div>
      )}
      {market.aggregates.map(/* aggregateCard 渲染单个词的扫描摘要卡片。 */ aggregate => (
        <div key={aggregate.keyword} className="ios-card rounded-2xl p-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <span className="text-sm font-extrabold text-gray-900">{aggregate.keyword}</span>
              <span className="ml-2 text-xs text-gray-400">{aggregate.date} · {aggregate.totalCount} 件商品 · want {aggregate.totalWant}</span>
            </div>
            <button
              type="button"
              onClick={/* 打开该词的商品列表动作。 */ () => void market.openItemList(aggregate.keyword)}
              className="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-600 hover:bg-gray-200"
            >
              查看商品
            </button>
          </div>
          {market.itemListKeyword === aggregate.keyword && (
            <div className="mt-3 max-h-[400px] space-y-1.5 overflow-y-auto">
              {market.itemListLoading && <div className="py-4 text-center text-xs text-gray-400">正在加载商品列表...</div>}
              {market.itemListError && <div className="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700">{market.itemListError}</div>}
              {market.itemList.map(/* itemListRow 渲染单个商品行。 */ item => (
                <div key={item.itemId} className="flex items-center gap-2 rounded-xl bg-gray-50 px-3 py-2">
                  <span className="min-w-0 flex-1 truncate text-xs font-bold text-gray-800">{item.title}</span>
                  <span className="shrink-0 text-xs text-red-500">¥{item.price}</span>
                  <span className="shrink-0 text-[10px] text-gray-400">{item.wantCountText || '-'}</span>
                  <button
                    type="button"
                    onClick={/* 查看详情动作。 */ () => void competitor.openDetail(item.itemId)}
                    className="shrink-0 rounded-md bg-brand px-2 py-1 text-[10px] font-extrabold text-white hover:bg-brand-highlight"
                  >
                    查看详情
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
      <SellerBoardPanel board={market.board} loading={market.boardLoading} error={market.boardError} />
      {createPortal(
        <CompetitorDetailModal
          open={competitor.detail !== null || competitor.detailLoading || competitor.detailError !== ''}
          detail={competitor.detail}
          loading={competitor.detailLoading}
          error={competitor.detailError}
          rewritten={competitor.rewritten}
          rewriting={competitor.rewriting}
          analysis={competitor.analysis}
          analyzing={competitor.analyzing}
          carrying={competitor.carrying}
          carryResult={competitor.carryResult}
          onClose={competitor.closeDetail}
          onRewrite={/* AI 改写动作。 */ () => { if (competitor.detail) void competitor.rewrite(competitor.detail.itemId); }}
          onAnalyze={/* AI 分析动作。 */ () => { if (competitor.detail) void competitor.analyze(competitor.detail.itemId); }}
          onCarry={/* 一键搬运动作。 */ () => { if (competitor.detail) void competitor.carry(competitor.detail.itemId); }}
        />,
        document.body,
      )}
    </div>
  );
};

export default MarketIntelSection;
