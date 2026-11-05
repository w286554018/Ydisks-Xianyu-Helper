import React from 'react';
import { Radar, ScanSearch } from 'lucide-react';
import { KeywordAggregatesTable, KeywordManagerPanel, KeywordTrendPanel, SellerBoardPanel } from '../components/MarketIntelPanels';
import { MomentumPanel } from '../components/MomentumPanel';
import { useMarketIntel } from '../hooks';

/** MarketIntelSection 是选品参考页的市场情报区块：关注词、热词涨幅、趋势与竞品卖家。 */
const MarketIntelSection: React.FC = () => {
  // market 是区块全部状态与动作。
  const market = useMarketIntel();

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
      <SellerBoardPanel board={market.board} loading={market.boardLoading} error={market.boardError} />
    </div>
  );
};

export default MarketIntelSection;
