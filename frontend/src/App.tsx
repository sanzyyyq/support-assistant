import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { ChatPage } from './pages/ChatPage';
import { OperatorPage } from './pages/OperatorPage';
import { AnalyticsPage } from './pages/operator/AnalyticsPage';
import { OperatorLayout } from './pages/operator/OperatorLayout';
import { ProfilePage } from './pages/operator/ProfilePage';
import { QuotesPage } from './pages/operator/QuotesPage';
import { RequireOperator } from './pages/operator/RequireOperator';
import { StaffPage } from './pages/operator/StaffPage';
import { StorePage } from './pages/StorePage';

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Клиент: витрина с виджетом и полноэкранный чат */}
        <Route path="/" element={<StorePage />} />
        <Route path="/chat" element={<ChatPage />} />
        {/* Менеджер — только после входа */}
        <Route
          path="/operator"
          element={
            <RequireOperator>
              <OperatorLayout />
            </RequireOperator>
          }
        >
          <Route index element={<OperatorPage />} />
          <Route path="quotes" element={<QuotesPage />} />
          <Route path="analytics" element={<AnalyticsPage />} />
          <Route path="staff" element={<StaffPage />} />
          <Route path="profile" element={<ProfilePage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
