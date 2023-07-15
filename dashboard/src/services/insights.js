import axios from "axios";

const baseUrl = process.env.REACT_APP_INSIGHTS_API;

const getInsights = async ({ style }) => {
  const body = {
    style,
  };

  const response = await axios.put(`${baseUrl}/api/insights`, body);
  return response.data;
};

const insightsService = {
  getInsights,
};

export default insightsService;
