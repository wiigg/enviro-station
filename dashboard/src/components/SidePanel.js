import { useState, useEffect } from "react";
import Spinner from "react-spinners/PulseLoader";

import insightsService from "../services/insights";
import inicon from "../images/insights.png";

const SidePanel = ({ isOpen, setIsOpen }) => {
  const [styleInput, setStyleInput] = useState("");
  const [isFormSubmitted, setIsFormSubmitted] = useState(false);
  const [textArray, setTextArray] = useState([]);
  const [currentWordIndex, setCurrentWordIndex] = useState(0);
  const [isLoading, setIsLoading] = useState(false);

  // set the ChatGPT-like interval
  useEffect(() => {
    const interval = setInterval(() => {
      setCurrentWordIndex((prevIndex) =>
        prevIndex < textArray.length ? prevIndex + 1 : prevIndex
      );
    }, 100);

    return () => {
      clearInterval(interval);
    };
  }, [textArray]);

  const handleInputChange = (event) => {
    setStyleInput(event.target.value);
  };

  const handleSubmit = async (event) => {
    event.preventDefault();
    setIsLoading(true);

    try {
      const styleInput = event.target.style.value;
      const response = await insightsService.getInsights({ style: styleInput });

      setTextArray(response.split(" ")); // Split the response into words
      setIsFormSubmitted(true);
      setIsLoading(false);
    } catch (error) {
      console.log(error);
      alert("Error: Unable to generate insights. Please try again later.");
      setIsLoading(false);
    }
  };

  const handleClear = () => {
    setIsFormSubmitted(false);
    setStyleInput("");
    setTextArray([]);
    setCurrentWordIndex(0);
  };

  const displayedResponse = textArray.slice(0, currentWordIndex).join(" ");

  return (
    <div
      className={`fixed inset-y-0 right-0 transform transition-transform duration-200 ease-in-out ${
        isOpen ? "translate-x-0" : "translate-x-full"
      } max-w-md w-full bg-gray-100 p-4 shadow-2xl text-white px-8 flex flex-col justify-between`}
    >
      <div className="overflow-auto pb-20">
        <button
          className="mb-5 hover:text-gray-500 text-gray-800 font-medium text-3xl p-1 rounded-full float-right"
          onClick={() => setIsOpen(false)}
        >
          x
        </button>
        <div className="flex items-center mb-2 mt-8">
          <img src={inicon} alt="icon" className="mr-2 h-8" />
          <h2 className="text-lg uppercase tracking-wider text-gray-800">
            Generate Insights
          </h2>
        </div>
        <div className="text-xs text-gray-500 mb-8">
          Disclaimer: Any insights should not be taken as actual advice. Please
          consult an expert before taking any actions.
        </div>
        {isFormSubmitted ? (
          <div>
            <p className="text-base text-gray-800 mt-8 mb-2">
              {displayedResponse}
            </p>
          </div>
        ) : (
          <div>
            <div className="text-base text-gray-800 mb-4">
              Generate insights and advice based on the latest environmental
              data! Simply enter the character or tone of the insights you would
              like to generate.
            </div>
            <form onSubmit={handleSubmit} className="space-y-2">
              <div>
                <label
                  htmlFor="style"
                  className="block text-base font-bold mb-2 text-gray-800"
                >
                  Style or Tone
                </label>
                <input
                  type="text"
                  id="style"
                  name="style"
                  value={styleInput}
                  onChange={handleInputChange}
                  className="shadow appearance-none border rounded w-full py-2 px-3 mb-2 text-gray-700 leading-tight focus:outline-none focus:shadow-outline"
                />
              </div>
              <button
                type="submit"
                className="bg-purple-500 hover:bg-purple-700 text-white font-bold py-2 px-4 rounded float-right"
                disabled={isLoading}
              >
                {isLoading ? <Spinner size={8} color="white" /> : "Generate"}
              </button>
            </form>
          </div>
        )}
      </div>
      {isFormSubmitted && (
        <button
          className="bg-yellow-500 hover:bg-yellow-700 text-white font-bold py-2 px-4 rounded"
          onClick={handleClear}
        >
          Return
        </button>
      )}
    </div>
  );
};

export default SidePanel;
