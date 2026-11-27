import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { ChatPage } from './pages/ChatPage';
import { OperatorPage } from './pages/OperatorPage';
import { AnalyticsPage } from './pages/operator/AnalyticsPage';
import { OperatorLayout } from './pages/operator/OperatorLayout';
import { QuotesPage } from './pages/operator/QuotesPage';
import { StorePage } from './pages/StorePage';

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Клиент: витрина с виджетом и полноэкранный чат */}
        <Route path="/" element={<StorePage />} />
        <Route path="/chat" element={<ChatPage />} />
        {/* Менеджер */}
        <Route path="/operator" element={<OperatorLayout />}>
          <Route index element={<OperatorPage />} />
          <Route path="quotes" element={<QuotesPage />} />
          <Route path="analytics" element={<AnalyticsPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
