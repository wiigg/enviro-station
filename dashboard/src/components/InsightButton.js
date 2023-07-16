const InsightButton = ({ setIsPanelOpen, text }) => (
  <button
    className="fixed bottom-5 right-4 bg-red-400 hover:bg-red-600 text-white p-4 rounded-full shadow-xl uppercase"
    onClick={() => setIsPanelOpen(true)}
  >
    {text}
  </button>
);

export default InsightButton;
