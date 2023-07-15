const InsightButton = ({ setIsPanelOpen, text }) => (
  <button
    className="fixed bottom-5 right-4 bg-red-500 hover:bg-red-700 text-white p-4 rounded-3xl shadow-xl uppercase"
    onClick={() => setIsPanelOpen(true)}
  >
    {text}
  </button>
);

export default InsightButton;
